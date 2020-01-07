#!/usr/bin/ruby

require 'nokogiri'
require 'fileutils'
require 'date'
require 'json'
require 'set'
require 'securerandom'

$data_dir = ENV['DATA_DIR'] || '/db'
if !File.directory?( $data_dir ) then
  raise "ERROR: set DATA_DIR environment variable"
end

def join( s1, s2 )
  return "#{s1.gsub(/\/$/,'')}/#{s2.gsub(/^\//,'')}"
end

# wikicode: np. 'plwiki', 'enwikiquote'
def last_dump_status( wikicode, jobs )
  result = Array.new
  main_url = "https://dumps.wikimedia.org/#{wikicode}/"
  curl_cmd = "curl -sL '#{main_url}'"
  doc = Nokogiri::HTML( `#{curl_cmd}` )
  dates = Array.new
  doc.css( 'a' ).each do |a|
    a.text.match(/^(\d{8})/){|m| dates.push(m[1])}
  end
  if dates.empty? then
    result.push( { 'wikicode' => wikicode, 'status' => 'ERROR', 'comments' => "Command '#{curl_cmd}' returned something unexpected, or network failure" } )
  else
    done_flag = false
    dates.sort.reverse.each do |date_s|
      if !done_flag then
        begin
          url = "#{main_url}#{date_s}/dumpstatus.json"
          json = JSON.parse( `curl -sL '#{url}'` )
          invalid_keys = Array.new
          json['jobs'].each do |key,obj|
            if (jobs.include?('all') || jobs.include?(key)) && (!obj['status'] || obj['status'].downcase != 'done') then
              invalid_keys.push( key )
            end
          end
          record = { 'wikicode' => wikicode, 'date' => Date.strptime(date_s, '%Y%m%d').to_s }
          if invalid_keys.empty? then 
            record['status'] = 'OK'
            done_flag = true
          else
            record['status'] = 'NOT READY'
            record['comments'] = invalid_keys.join(',')
          end
          result.push( record )
        rescue Exception => e
          STDERR.puts e
        end
      end
    end
  end
  return result
end

def valid_dump?( dump_path )
  return dump_valid_status(dump_path)['valid']
end

def dump_valid_status( dump_path )
  result = { 'valid' => true, 'invalid-phases' => [] }
  Dir.glob( File.join( dump_path, '*-logs.txt' ) ).each do |lpath|
    phase = lpath.gsub(/^.*\/([^\/]+?)-logs.txt$/, '\\1')
    valid = `tail -1 '#{lpath}'` =~ /^OK$/
    if !valid then
      result['valid'] = false
      result['invalid-phases'].push( "!#{phase}" )
    end
  end
  return result
end

def remove_old_dumps()
  ['wikipedia', 'wikiquote'].each do |wiki_type|
    dir = File.join( $data_dir, 'cache', 'dumps', wiki_type )
    public_dir = File.join( $data_dir, 'public', wiki_type )
    Dir.glob( File.join(dir, '*') ).select{|f| File.directory?( f )}.each do |dump_path| # np. '${DATA_DIR}/cache/dumps/wikipedia/pl'
      lang = File.basename( dump_path )
      hash = get_dump_info( dump_path )
      puts "#{dump_path}\t#{JSON.generate( hash )}"
      hash['to-remove'].each do |dir_name| 
        dir_to_remove = File.join( dump_path, dir_name )
        print "Removing '#{dir_to_remove}'... "
        s = `rm -fr #{dir_to_remove}`
        status = $?.exitstatus
        puts "Done with exit code #{status}"
        if status != 0 then STDERR.puts s end

        public_dir_to_remove = File.join( public_dir, lang, dir_name )
        print "Removing '#{public_dir_to_remove}'... "
        s = `rm -fr #{public_dir_to_remove}`
        status = $?.exitstatus
        puts "Done with exit code #{status}"
        if status != 0 then STDERR.puts s end
      end
    end
  end
end

# Dla danego katalogu projektu jezykowego Wikimedia (np. cache/dumps/wikipedia/pl) zwraca informacje o dostępnych dumpach
# @return { 'current' => '20191201', 'previous' => '20191120', 'latest' => '20191220', 'latest-valid': '20191220', 'to-remove': ['20191101', '20191020'] }
# (niektórych kluczy może nie być, zawsze będzie 'to-remove' z tablicą, być może pustą).
def get_dump_info( lang_path )
  result = { 'to-remove' => [], 'info' => [] }
  ['current', 'previous'].each do |name|
    path = File.join( lang_path, name )
    if File.symlink?( path ) then
      result[name] = File.readlink( path )
    end
  end
  Dir.glob( File.join( lang_path, '*' ) ).select{|path| File.basename(path) =~ /^[0-9]{8}$/}.sort.reverse.each do |dump_path|
    basename = File.basename( dump_path )
    if !result.has_key?( 'latest' ) then result['latest'] = basename end
    if !result.has_key?( 'latest-valid' ) && valid_dump?( dump_path ) then result['latest-valid'] = basename end
  end
  Dir.glob( File.join( lang_path, '*' ) ).select{|path| File.basename(path) =~ /^[0-9]{8}$/}.sort.reverse.each do |dump_path|
    basename = File.basename( dump_path )
    record = {'version' => basename, 'tags' => [] }
    ['current', 'previous', 'latest', 'latest-valid'].each do |key|
      if result.has_key?( key ) && result[key] == basename then
        record['tags'].push( key )
      end
    end
    if record['tags'].empty? then
      result['to-remove'].push( basename )
    end
    result['info'].push( record )
  end
  return result
end

# Wypisuje na STDOUT informacje o całym drzewie katalogów $data_dir
def print_data_dir_info()
  ['wikipedia', 'wikiquote'].map{|proj_name| File.join($data_dir, 'cache', 'dumps', proj_name)}.each do |dir|
    Dir.glob( File.join(dir, '*') ).select{|f| File.directory?( f )}.each do |lang_path|
      puts "#{lang_path[$data_dir.length..-1]}:"
      hash = get_dump_info( lang_path )
      hash['info'].each do |rec|
        dump_path = File.join( lang_path, rec['version'] )
        valid_status = dump_valid_status( dump_path )
        puts "\t#{rec['version']}\t#{valid_status['valid'] ? 'OK' : valid_status['invalid-phases'].join(',')}\t#{rec['tags'].join(',')}"
      end
    end
  end  
  puts
  cmd = "du -h #{$data_dir}"
  puts "$ #{cmd}"
  puts `#{cmd}`
end

# Zaciąga brakujące pliki clickstream
def download_clickstream( months_count, lc=nil )
  clickstream_dir = File.join( $data_dir, 'cache', 'clickstream' )
  # dokumentacja: https://dumps.wikimedia.org/other/clickstream/readme.html
  main_url = 'https://dumps.wikimedia.org/other/clickstream/'
  doc = Nokogiri::HTML( `curl -sL '#{main_url}'` )
  arr = Array.new
  doc.css( 'a' ).each do |a|
    a.text.match( /^(\d{4}-\d{2})\// ) do |m|
      arr.push( { 'date' => m[1], 'url' => join( main_url, a['href'] ) } )
    end
  end
  arr = arr.sort{|r1,r2| r1['date'] <=> r2['date'] }.reverse
  if months_count > 0 then arr = arr[0...months_count] end
  # Usuwamy stare tymczasowe pliki
  Dir[File.join(clickstream_dir, 'tmp-*.tmp')].each do |fpath|
    puts "Removing #{fpath}"
    File.unlink( fpath )
  end
  tmpfname = "tmp-#{SecureRandom.hex(6)}.tmp"
  tmp_fpath = File.join( clickstream_dir, tmpfname )
  arr.each do |rec|
    output_dir = File.join( clickstream_dir, rec['date'] )
    doc = Nokogiri::HTML( `curl -sL '#{rec['url']}'` )
    doc.css( 'a' ).each do |a|
      if a.text =~ /\.gz/ then
        file_name = a['href']
        output_fpath = File.join( output_dir, file_name )
        if !File.exist?( output_fpath ) then
          start_ts = Time.now
          file_url = join( rec['url'], file_name )
          print "Downloading #{file_url}... "
          `wget -q '#{file_url}' -O '#{tmp_fpath}'`
          status = $?.exitstatus
          if status == 0 then
            FileUtils.mkdir_p( output_dir )
            File.rename( tmp_fpath, output_fpath )
            puts "Done [#{span_s(start_ts)}]"
          else
            `rm '#{tmp_fpath}'`
            STDERR.puts "Failed with exitcode #{status} [#{span_s(start_ts)}]"
          end
        end
      end
    end
  end
  remove_old_clickstream_files( months_count )
end

def remove_old_clickstream_files( months_count )
  if months_count <= 0 then return end
  clickstream_dir = File.join( $data_dir, 'cache', 'clickstream' )
  arr = Array.new
  Dir[File.join(clickstream_dir, '*')].select{|f| File.directory?(f) && (f =~ /\d{4}-\d{2}$/)}.sort.reverse[months_count..-1].each do |fpath|
    puts "Removing old clickstream dir: '#{fpath}'"
    FileUtils.remove_dir( fpath, true )
  end
end

# Dla podanej liczby sekund zwraca łańcuch "HH:MM:SS" od polnocy
def span_s( start_ts )
  return Time.at(Time.now - start_ts).gmtime.strftime("%H:%M:%S")
end

# Dokumentacja: https://dumps.wikimedia.org/other/pagecounts-ez/
def download_pageview_months( months_count )
  main_url = 'https://dumps.wikimedia.org/other/pagecounts-ez/merged/'
  mpageview_dir = File.join( $data_dir, 'cache', 'pageview', 'months')
  FileUtils.mkdir_p( mpageview_dir )
  # Wyciągamy listę plików dostępnych w Internecie: po jednym dla każdego miesiąca
  doc = Nokogiri::HTML( `curl -sL '#{main_url}'` )
  arr = Array.new
  doc.css( 'a' ).each do |a|
    a.text.match( /pagecounts-(\d{4}-\d{2})-views-ge-5-totals\.bz2/ ) do |m| 
      arr.push({'date'=>m[1], 'filename' => a['href'], 'url' => join( main_url, a['href'] )})
    end
  end  
  arr = arr.sort{|r1,r2| r1['date'] <=> r2['date'] }.reverse
  if months_count > 0 then arr = arr[0...months_count] end
  # Usuwamy stare tymczasowe pliki
  Dir[File.join(mpageview_dir, 'tmp-*.tmp')].each do |fpath|
    puts "Removing #{fpath}"
    File.unlink( fpath )
  end
  tmpfname = "tmp-#{SecureRandom.hex(6)}.tmp"
  tmp_fpath = File.join( mpageview_dir, tmpfname )
  # Ściągamy pliki
  arr.each do |rec|
    output_fpath = File.join( mpageview_dir, rec['filename'] )
    if !File.exist?( output_fpath ) then # ... jeśli jeszcze nie ma
      start_ts = Time.now
      print "Downloading #{rec['url']}... "
      `wget -q '#{rec['url']}' -O '#{tmp_fpath}'`
      status = $?.exitstatus
      if status == 0 then
        File.rename( tmp_fpath, output_fpath )
        puts "Done [#{span_s(start_ts)}]"
      else
        `rm '#{tmp_fpath}'`
        STDERR.puts "Failed with exitcode #{status} [#{span_s(start_ts)}]"
      end
    end
  end
  remove_old_pageview_files( months_count, 'months' )
end

def download_pageview_days( days_count )
  dpageview_dir = File.join( $data_dir, 'cache', 'pageview', 'days')
  FileUtils.mkdir_p( dpageview_dir )
  # Usuwamy stare tymczasowe pliki
  Dir[File.join(dpageview_dir, 'tmp-*.tmp')].each do |fpath|
    puts "Removing #{fpath}"
    File.unlink( fpath )
  end
  yesterday = Date.today-1
  date = yesterday
  counter = 0
  tmpfname = "tmp-#{SecureRandom.hex(6)}.tmp"
  tmp_fpath = File.join( dpageview_dir, tmpfname )
  while (date.to_s >= '2011-11-16') && ((days_count <= 0) || (counter < days_count)) do
    year, month, day = date.strftime('%Y'), date.strftime('%m'), date.strftime('%d')
    fname = "pagecounts-#{year}-#{month}-#{day}.bz2"
    output_fpath = File.join( dpageview_dir, fname )
    if File.exist?( output_fpath ) then
      counter += 1
    else
      start_ts = Time.now
      url = "https://dumps.wikimedia.org/other/pagecounts-ez/merged/#{year}/#{year}-#{month}/#{fname}"
      print "Downloading #{url}... "
      `wget -q '#{url}' -O '#{tmp_fpath}'`
      status = $?.exitstatus
      if status == 0 then
        File.rename( tmp_fpath, output_fpath )
        counter += 1
        puts "Done [#{span_s(start_ts)}]"
      else
        `rm '#{tmp_fpath}'`
        if date.to_s == yesterday.to_s && status == 8 then
          puts "Resource not exist yet?"
        else
          STDERR.puts "Failed with exitcode #{status} [#{span_s(start_ts)}]"
        end
      end
    end
    date -= 1
  end
  remove_old_pageview_files( days_count, 'days' )
end

def download_pageview_hours( hours_count )
  arr = []
  date = Date.today
  while arr.size < hours_count do
    year, month = date.strftime('%Y'), date.strftime('%m')
    url = "https://dumps.wikimedia.org/other/pageviews/#{year}/#{year}-#{month}"
    html = `curl -sL '#{url}'`
    status = $?.exitstatus
    if status == 0 then
      doc = Nokogiri::HTML( html )
      arr = Array.new
      doc.css( 'a' ).each do |a|
        a.text.match( /pageviews-(\d{8}-\d{2})0000.gz/ ) do |m|
          arr.push({'date'=>m[1], 'filename' => a['href'], 'url' => join( url, a['href'] )})
        end
      end
      arr = arr.sort{|r1,r2| r1['date'] <=> r2['date'] }.reverse
    end
    loop do
      date = date - 1
      if date.strftime( '%m' ) != month then break end
    end
  end
  arr = arr[0...hours_count]
  # W tym miejscu w arr jest lista hours_count najnowszych plikow hours pageview, posortowanych malejaco
  hpageview_dir = File.join( $data_dir, 'cache', 'pageview', 'hours' )
  FileUtils.mkdir_p( hpageview_dir )
  # Usuwamy stare tymczasowe pliki
  Dir[File.join(hpageview_dir, 'tmp-*.tmp')].each do |fpath|
    puts "Removing #{fpath}"
    File.unlink( fpath )
  end
  tmpfname = "tmp-#{SecureRandom.hex(6)}.tmp"
  tmp_fpath = File.join( hpageview_dir, tmpfname )
  # Dociągamy brakujące pliki
  arr.each do |rec|
    output_fpath = File.join( hpageview_dir, rec['filename'] )
    if !File.exist?( output_fpath ) then
      start_ts = Time.now
      url = rec['url']
      print "Downloading #{url}... "
      `wget -q '#{url}' -O '#{tmp_fpath}'`
      status = $?.exitstatus
      if status == 0 then
        File.rename( tmp_fpath, output_fpath )
        puts "Done [#{span_s(start_ts)}]"
      else
        `rm '#{tmp_fpath}'`
        puts "Failed with exitcode #{status} [#{span_s(start_ts)}]"
      end
    end
  end
  remove_old_pageview_files( hours_count, 'hours', 'gz' )
end

def remove_old_pageview_files( count, dir_name, ext="bz2" )
  if count <= 0 then return end
  pageview_subdir = File.join( $data_dir, 'cache', 'pageview', dir_name)
  if File.directory?( pageview_subdir ) then
    Dir[File.join(pageview_subdir, "*.#{ext}")].sort.reverse[count..-1].each do |fpath|
      puts "Removing old pageview file: '#{fpath}'"
      File.unlink( fpath )
    end
  end
end

def usage
  STDERR.puts "Options examples:"
  STDERR.puts "   pageview-months [count]"
  STDERR.puts "     Ściąga <count> (domyślnie 26, 0 oznacza wszystkie) najnowsze pliki"
  STDERR.puts "     https://dumps.wikimedia.org/other/pagecounts-ez/merged/pagecounts-YYYY-MM-views-ge-5-totals.bz2"
  STDERR.puts "     do $DATA_DIR/cache/pageview/months/ i usuwa starsze"
  STDERR.puts "   pageview-days [count]"
  STDERR.puts "     Ściąga <count> (domyślnie 64, 0 oznacza wszystkie) najnowsze pliki"
  STDERR.puts "     https://dumps.wikimedia.org/other/pagecounts-ez/merged/YYYY/YYYY-MM/pagecounts-YYYY-MM-DD.bz2"
  STDERR.puts "     do $DATA_DIR/cache/pageview/days/ i usuwa starsze"
  STDERR.puts "   pageview-hours count"
  STDERR.puts "     Ściąga <count> (domyślnie 26) najnowsze pliki"
  STDERR.puts "     https://dumps.wikimedia.org/other/pageviews/YYYY/YYYY-MM/projectviews-YYYYMMDD-HH0000"
  STDERR.puts "     do $DATA_DIR/cache/pageview/hours/ i usuwa starsze"
  STDERR.puts "   clickstream [count]"
  STDERR.puts "     Ściąga pliki https://dumps.wikimedia.org/other/clickstream/YYYY-MM/clickstream-dewiki-MMMM-MM.tsv.gz"
  STDERR.puts "     z ostatnich count miesięcy (0 - wartość domyślna - oznacza wszystkie),"
  STDERR.puts "     do katalogu $DATA_DIR/cache/clickstream/YYYY-MM/ i usuwa najstarsze pliki"
  STDERR.puts "   remove-old-dumps"
  STDERR.puts "     usuwa $DATA_DIR/cache/dumps stare dumpy nie wskazywane przez linki symboliczne 'current' ani 'previous'"
  STDERR.puts "     i nie będące najnowszym lub najnowszym poprawnym dumpem"
  STDERR.puts "   last-dump-status <jobs> <format> <wikicodes>"
  STDERR.puts "     <jobs>: rozdzielone przecinkami nazwy jobów z pliku dumpstatus.json generowanego razem z dumpem"
  STDERR.puts "       (np. https://dumps.wikimedia.org/plwiki/20191220/dumpstatus.json),"
  STDERR.puts "       albo 'all' gdy wszystkie joby muszą mieć status 'done'"
  STDERR.puts "     <format> format wyniku na STDOUT: 'tsv' lub 'json'"
  STDERR.puts "     <wikicodes>: rozdzielone przecinkami nazwy projektow wiki, np. 'plwiki,enwikiquote,eswiki'"
  STDERR.puts "   data-dir-info"
  STDERR.puts "     wypisuje na STDOUT informacje o całej strukturze katalogu $DATA_DIR"
  if !STDOUT.isatty then 
    STDOUT.flush
    STDERR.puts "== END at #{Time.now.to_s} [#{span_s($app_start_ts)}] ==" 
    STDERR.flush
  end
  exit 1
end


$app_start_ts = Time.now
if !STDOUT.isatty then
  STDOUT.flush
  STDERR.puts "== BEGIN at #{$app_start_ts.to_s} #{ARGV.join(' ')} =="
  STDERR.flush
end
if ARGV.length < 1 then
  usage
elsif ARGV[0] == 'clickstream' then
  months_count = 0
  if ARGV.length > 2 then usage end
  if ARGV.length == 2 then
    if ARGV[1] !~ /^\d+$/ then 
      usage
    else
      months_count = ARGV[1].to_i
    end
  end
  download_clickstream( months_count )
elsif ARGV[0] == 'pageview-months' then
  months_count = 26
  if ARGV.length > 2 then usage end
  if ARGV.length == 2 then
    if ARGV[1] !~ /^\d+$/ then
      usage
    else
      months_count = ARGV[1].to_i
    end
  end
  download_pageview_months( months_count )
elsif ARGV[0] == 'pageview-days' then
  days_count = 64
  if ARGV.length > 2 then usage end
  if ARGV.length == 2 then
    if ARGV[1] !~ /^\d+$/ then
      usage
    else
      days_count = ARGV[1].to_i
    end
  end
  download_pageview_days( days_count )
elsif ARGV[0] == 'pageview-hours' then
  days_count = 26
  if ARGV.length > 2 then usage end
  if ARGV.length == 2 then
    if ARGV[1] !~ /^\d+$/ then
      usage
    else
      hours_count = ARGV[1].to_i
    end
  end
  download_pageview_hours( hours_count )
elsif ARGV[0] == 'last-dump-status' then
  if ARGV.length != 4 || ARGV[1] !~ /^[a-z,]+$/ || ARGV[2] !~ /^(tsv|json)$/ || ARGV[3] !~ /^[a-z,]+$/ then usage end
  result = Array.new
  jobs = ARGV[1].split(/,/).to_set
  ARGV[3].split(/,/).each do |wikicode|
    result = result.concat( last_dump_status( wikicode, jobs ) )
  end
  if ARGV[2] == 'json' then
    puts JSON.generate( result )
  else
    result.each do |record|
      puts "#{record['wikicode']}\t#{record['date']||''}\t#{record['status']}\t#{record['comments']||''}"
    end
  end
elsif ARGV[0] == 'remove-old-dumps' then
  remove_old_dumps()
elsif ARGV[0] == 'data-dir-info' then
  print_data_dir_info()
else
  STDERR.puts 'Unrecognized command!'
  usage
end
if !STDOUT.isatty then 
  STDOUT.flush
  STDERR.puts "== END at #{Time.now.to_s} [#{span_s($app_start_ts)}] ==" 
  STDERR.flush
end
