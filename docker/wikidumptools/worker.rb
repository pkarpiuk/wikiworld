#!/usr/bin/ruby

require 'date'
require 'time'
require 'fileutils'
require 'json'
require 'set'

# Ustala daty najnowszych dumpow dla wskazanych projektow Wikimedia
# wikicodes: Set z kodami projektow, np. ['plwiki', 'enwikiquote', 'eswiki'].to_set
# @return: mapa <kod_projektu>: <YYYY-MM-DD> dla tych projektow, dla ktorych jest jakis mozliwie najswiezszy dump
def get_last_dump_dates( wikicodes )
  result = Hash.new
  jobs = ['pagetable', 'redirecttable', 'abstractsdumprecombine', 'geotagstable', 'categorylinkstable', 'pagerestrictionstable',
          'externallinkstable', 'imagelinkstable', 'langlinkstable', 'pagepropstable', 'pagelinkstable', 'templatelinkstable']
  s, status = docker_run( "docker run --rm wiki-extra-download last-dump-status '#{jobs.join(',')}' json '#{wikicodes.to_a.join(',')}'", true )
  JSON.parse( s ).each do |rec|
    wikicode = rec['wikicode']
    date = (rec['date'] || '').gsub( /\D/, '' )
    status = rec['status']
    msg = "  #{wikicode}\t#{date}\t#{status}\t#{rec['comments']}"
    if status == 'OK' then
      puts msg
      result[wikicode] = date
    else
      STDERR.puts msg
    end
  end
  return result
end

# Dla podanej liczby sekund zwraca łańcuch "HH:MM:SS" od polnocy
def span_s( start_ts )
  return Time.at(Time.now - start_ts).gmtime.strftime("%H:%M:%S")
end

# Uruchamia wskazane polecenie Dockera, mierzy i wyswietla czas wykonania i kod bledu wykonania.
# raise_at_fail: czy zrzucic wyjatek gdy polecenie Dockera zakonczy sie kodem bledu innym niz 0
# @return [result, status] STDOUT wyniku polecenia Dockera (string) i kod bledu wykonania (int)
def docker_run( docker_cmd, raise_at_fail=false )
  start_ts = Time.now
  # "docker run --rm -v '#{$DATA_DIR}':/db wiki-extra-download pageview-months #{$PAGEVIEW_MONTHS} >> '#{months_log_fpath}' 2>&1"
  # "docker run --rm -v '#{$DATA_DIR}':/db wikidumptools --dump=#{dump_date} extract net #{lc}#{suffix} tsv"
  puts docker_cmd

  docker_cmd.gsub!(/^.*\swiki-extra-download\s/, "./wiki-extra-download.rb ")
  docker_cmd.gsub!(/^.*\swikidumptools\s/, "./main ")
  puts docker_cmd

  result = `#{docker_cmd}`
  status = $?.exitstatus
  puts "STATUS: #{status}"
  if status == 0 then
    puts "Done [#{span_s(start_ts)}]"
  else
    msg = "Failed with exit status #{status} [#{span_s(start_ts)}]"
    if raise_at_fail then
      raise msg
    else
      STDERR.puts msg
    end
  end
  return [result, status]
end

def get_log_path( dump_path, phase_name )
  log_fname = "#{phase_name}-logs.txt"
  return File.join( dump_path, log_fname )
end

def wikidumptools_run( dump_path, phase_name, docker_cmd )
  log_path = get_log_path( dump_path, phase_name )
  if File.exists?( log_path ) then
    if (`tail -1 '#{log_path}'` =~ /^OK$/) then
      puts "Operation '#{phase_name}' in #{dump_path} looks completed, skip"
      return 0
    else
      STDERR.puts "File '#{log_path}' exists, but have not 'OK' at the end; Remake..."
    end
  end
  docker_cmd = "#{docker_cmd} > '#{log_path}' 2>&1"
  result, status = docker_run( docker_cmd )
  if status == 0 then
    `echo 'OK' >> '#{log_path}'`
  end
  return status
end

def extract_and_cathier( dates_map, lc, suffix, wiki_type )
  if dates_map.has_key?( "#{lc}#{suffix}" ) then
    dump_date = dates_map["#{lc}#{suffix}"]
    wiki_dir = File.join($DATA_DIR, 'cache', 'dumps', wiki_type, lc)
    wiki_public_dir = File.join($DATA_DIR, 'public', wiki_type, lc)
    dump_dir = File.join(wiki_dir, dump_date)
    FileUtils.mkdir_p( dump_dir )
    PHASE_NAMES.each do |phase_name|
      if !((suffix == 'wikiquote') && [PHASE_NAME_GEOMAP, PHASE_NAME_CLICKSTREAM].include?(phase_name)) then
        log_path = get_log_path( dump_dir, phase_name )
        `touch '#{log_path}'`
      end
    end
    status = 0
    counter = 0
    while true
      counter += 1
      status = wikidumptools_run( dump_dir, PHASE_NAME_EXTRACT, "docker run --rm -v '#{$DATA_DIR}':/db wikidumptools --dump=#{dump_date} extract net #{lc}#{suffix} tsv" )
      if status == 0 || counter > 3 then
        break
      else
        sleep 60
      end
    end
    if status == 0 then
      status = wikidumptools_run( dump_dir, PHASE_NAME_MONTHS_PAGEVIEW, "docker run --rm -v '#{$DATA_DIR}':/db wikidumptools --dump=#{dump_date} months-pageview #{lc}#{suffix} 12" )
      status = wikidumptools_run( dump_dir, PHASE_NAME_CATHIER, "docker run --rm -v '#{$DATA_DIR}':/db wikidumptools --dump=#{dump_date} cathier #{lc}#{suffix} full" )
      if suffix != 'wikiquote' then
        status = wikidumptools_run( dump_dir, PHASE_NAME_GEOMAP, "docker run --rm -v '#{$DATA_DIR}':/db wikidumptools --dump=#{dump_date} geomap #{lc}#{suffix}" )
        status = wikidumptools_run( dump_dir, PHASE_NAME_CLICKSTREAM, "docker run --rm -v '#{$DATA_DIR}':/db wikidumptools --dump=#{dump_date} clickstream #{lc}#{suffix}" )
      end
      current_dir = File.join( wiki_dir, 'current' )
      current_public_dir = File.join( wiki_public_dir, 'current' )
      if !File.exist?( current_dir ) then
        File.symlink( dump_date, current_dir )
        begin
          File.unlink( current_public_dir )
        rescue
        end
        File.symlink( dump_date, current_public_dir )
      end
    end
  end
end

def check_environment
  reasons = Array.new
  if !$DATA_DIR || $DATA_DIR.empty? then reasons.push( 'DATA_DIR variable not defined' )
  elsif !File.directory?( $DATA_DIR ) then
    reasons.push( "DATA_DIR='#{$DATA_DIR}' does not exist or is not a directory" )
  end
  if $WIKIPEDIA_LANGUAGES.empty? && $WIKIQUOTE_LANGUAGES.empty? then
    reasons.push( "You should set environment variables: WIKIPEDIA_LANGUAGES or WIKIQUOTE_LANGUAGES" )
  end
  if !reasons.empty? then
    STDERR.puts "Cannot run this script. Reasons are:\n"
    reasons.each do |reason|
      STDERR.puts "  - #{reason}"
    end
    exit 1
  end
end

def daily_main()
  logs_dir = File.join( $DATA_DIR, 'logs' )
  if $PAGEVIEW_MONTHS >= 0 then
    puts "Downloading monthly pageviews"
    months_log_fpath = File.join( logs_dir, 'pageview-months-logs.txt' )
    result, status = docker_run( "docker run --rm -v '#{$DATA_DIR}':/db wiki-extra-download pageview-months #{$PAGEVIEW_MONTHS} >> '#{months_log_fpath}' 2>&1" )
  end
  if $CLICKSTREAM_MONTHS >= 0 then
    puts "Downloading monthly clickstream files"
    clickstream_log_fpath = File.join( logs_dir, 'clickstream-logs.txt' )
    result, status = docker_run( "docker run --rm -v '#{$DATA_DIR}':/db wiki-extra-download clickstream #{$CLICKSTREAM_MONTHS} >> '#{clickstream_log_fpath}' 2>&1" )
  end

  puts "Downloading new dumps"
  # Zaciągamy najnowsze dumpy (extract+cathier full)
  json = get_last_dump_dates( $WIKIPEDIA_LANGUAGES.map{|lc| lc+'wiki'}.concat($WIKIQUOTE_LANGUAGES.map{|lc| lc+'wikiquote'}).to_set )
  $WIKIPEDIA_LANGUAGES.each do |lc|
    extract_and_cathier( json, lc, 'wiki', 'wikipedia' )
  end
  $WIKIQUOTE_LANGUAGES.each do |lc|
    extract_and_cathier( json, lc, 'wikiquote', 'wikiquote' )
  end
  # Usuwamy stare dumpy
  puts "Removing old dumps"
  result, status = docker_run( "docker run --rm -v '#{$DATA_DIR}':/db wiki-extra-download remove-old-dumps 2>&1" )
  puts result
  if $PAGEVIEW_DAYS >= 0 then
    puts "Downloading daily pageviews"
    days_log_fpath = File.join( logs_dir, 'pageview-days-logs.txt' )
    result, status = docker_run( "docker run --rm -v '#{$DATA_DIR}':/db wiki-extra-download pageview-days #{$PAGEVIEW_DAYS} >> '#{days_log_fpath}' 2>&1" )
  end
  result, status = docker_run( "docker run --rm -v '#{$DATA_DIR}':/db wiki-extra-download data-dir-info > #{File.join($DATA_DIR, 'public', 'downloader-stats.txt')} 2>&1" )
end

def hourly_main()
  clickstream_dir = File.join( $DATA_DIR, 'cache', 'clickstream' )
  project_names = Set.new
  Dir[File.join(clickstream_dir, "**/*.gz")].each do |path| # clickstream-enwiki-2018-04.tsv.gz
    File.basename( path ).match(/clickstream-(\w+)-\d+/) do |m|
      project_names.add( m[1] )
    end
  end
  if project_names.size == 0 then
    puts "Clickstream not ready, waiting..."
    return
  end
  logs_dir = File.join( $DATA_DIR, 'logs' )
  log_path = get_log_path( logs_dir, 'hours-pageview' )
  docker_cmd = "docker run --rm -v '#{$DATA_DIR}':/db wikidumptools --dump=current hours-pageview > '#{log_path}' 2>&1"
  result, status = docker_run( docker_cmd )
end

def usage
  STDERR.puts "Usage: ./worker.rb (daily|hourly)"
  exit 1
end

$DATA_DIR = ENV['DATA_DIR']
$WIKIPEDIA_LANGUAGES = (ENV['WIKIPEDIA_LANGUAGES'] || '').strip.downcase.split(/\W+/)
$WIKIQUOTE_LANGUAGES = (ENV['WIKIQUOTE_LANGUAGES'] || '').strip.downcase.split(/\W+/)
$PAGEVIEW_MONTHS = (ENV['PAGEVIEW_MONTHS'] || '26').to_i
$PAGEVIEW_DAYS = (ENV['PAGEVIEW_DAYS'] || '64').to_i
$PAGEVIEW_HOURS = (ENV['PAGEVIEW_HOURS'] || '26').to_i
$CLICKSTREAM_MONTHS = (ENV['CLICKSTREAM_MONTHS'] || '0').to_i

PHASE_NAME_EXTRACT='extract'
PHASE_NAME_MONTHS_PAGEVIEW='months-pageview'
PHASE_NAME_CATHIER='cathier'
PHASE_NAME_GEOMAP='geomap'
PHASE_NAME_CLICKSTREAM='clickstream'
PHASE_NAMES=[PHASE_NAME_EXTRACT, PHASE_NAME_MONTHS_PAGEVIEW, PHASE_NAME_CATHIER, PHASE_NAME_GEOMAP, PHASE_NAME_CLICKSTREAM]

if ARGV[0] !~ /^(daily|hourly)$/ then usage() end

check_environment()

`./create-data-dir-hierarchy.sh`

def the_loop(timespan)
  while true do
    puts "MAIN LOOP #{DateTime.now.to_s}"
    start_ts = Time.now
    begin
      yield()
    rescue Exception => ex
      STDERR.puts ex
    end
    puts "END OF MAIN LOOP [#{span_s(start_ts)} s]"
    span_sec = (Time.now - start_ts).round
    delta = timespan - span_sec
    if delta > 0 then
      puts "Sleep for #{delta} seconds..."
      STDOUT.flush
      STDERR.flush
      sleep delta # odpalamy się średnio raz na timespan sekund
    end
  end
end

if ARGV[0] == 'daily' then
  the_loop(3600*24) do daily_main() end
else
  the_loop(3600) do hourly_main() end
end

