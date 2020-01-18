#!/usr/bin/ruby

require 'set'
require 'fileutils'
require 'zlib'
require 'cgi'
require 'date'
require 'json'

def usage()
  STDERR.puts "Usage: ./extra-downloader.rb [divider rest]"
  STDERR.puts "Ściąga informacje tylko o takich artykułach, których page_id przy dzieleniu przez <divider> dają resztę <rest>. Domyślnie divider=1, rest=0."
  exit 1
end

def curl(url, extra="")
  if extra && !extra.empty? then STDERR.puts( "#{url}#{extra}" ) end
  finished = false
  result = nil
  while true
    begin
      result = `curl -sL "#{url}#{extra}" --header "Content-Type: text/plain; charset=utf-8"`
      json = JSON.parse( result )
      finished = true
    rescue
      sleep 10
    end
    if finished then break end
  end
  return result
end

def process_language_inner( db_fpath, articles_fpath, lang, was_page_ids, geoset )
  line_counter = 0
  open( db_fpath, 'w') do |fout|
    Zlib::GzipReader.open( articles_fpath ) do |gz|
      while line = gz.gets do
        page_id, page_title, rest = line.split( /\t/, 3 )
        line_counter += 1
        if line_counter > 1 then
          page_id = page_id.to_i
          if line_counter % 10 == 0 then
            puts "#{lang} #{line_counter} #{page_id} #{page_title}"
          end
          if !$geoflag || geoset.include?( page_id ) then
            if page_id % $divider == $rest then
              if !was_page_ids.include?( page_id ) then
                record = { 'page_id' => page_id, 'download_ts' => DateTime.now.to_s }
                cgi_title = CGI::escape( page_title.gsub( ' ', '_' ) )
                json = JSON.parse( curl( "https://#{lang}.wikipedia.org/w/api.php?action=query&prop=revisions&rvlimit=1&rvprop=timestamp&rvdir=newer&format=json&formatversion=2&utf8=&pageids=#{page_id}") )
                record['create_ts'] = json['query']['pages'][0]['revisions'][0]['timestamp']
                record['summary'] = JSON.parse( curl( "https://#{lang}.wikipedia.org/api/rest_v1/page/summary/#{cgi_title}" ) )
                record['summary'].delete( 'content_urls' )
                record['summary'].delete( 'api_urls' )
                record['media'] = JSON.parse( curl( "https://#{lang}.wikipedia.org/api/rest_v1/page/media/#{cgi_title}" ) )
                record['media']['items'].each do |obj|
                  obj.delete( 'artist' )
                  obj.delete( 'credit' )
                  obj.delete( 'license' )
                end
                related = JSON.parse( curl( "https://#{lang}.wikipedia.org/api/rest_v1/page/related/#{cgi_title}" ) )['pages'] || []
                record['related'] = related.map{|rec| rec['pageid']}
                fout.puts JSON.generate( record )
                was_page_ids.add( page_id )
              end
            end
          end
        end
      end
    end
  end
end

def process_language( db_fpath, articles_fpath, lang, geo_fpath )
  was_page_ids = Set.new
  geoset = Set.new
  if $geoflag then
    Zlib::GzipReader.open( geo_fpath ) do |gz|
      while line = gz.gets do
        page_id, lat, lon, globe, primary, rest = line.split( /\t/, 6 )
        page_id = page_id.to_i
        if (primary == '1') && (page_id % $divider == $rest) then
          geoset.add( page_id )
        end
      end
    end
  end
  if File.exist?( db_fpath ) then
    File.open(db_fpath).each do |line|
      begin
        json = JSON.parse( line )
        pid = json['page_id']
        if pid % $divider == $rest then
          was_page_ids.add( pid )
        end
      rescue Exception => ex
        STDERR.puts "ERROR: #{ex}"
      end
    end
  end
  while true do
    begin
      process_language_inner( db_fpath, articles_fpath, lang, was_page_ids, geoset )
    rescue Exception => ex
      STDERR.puts ex
      exit 1
    end
    was_page_ids = Set.new
    puts "#{lang} NEW LOOP"
    break # TODO: usunąć
  end
end

$data_dir = ENV['DATA_DIR'] || '/db'
if !$data_dir || !File.directory?( $data_dir ) then raise "Set DATA_DIR environment variable" end

$geoflag = true # uwzględniamy tylko te page_id, które dotyczą miejsc w terenie

$divider = 1
$rest = 0
if ARGV.length > 0 then
  if ARGV.length == 2 && ARGV[0] =~ /^[0-9]+$/ && ARGV[1] =~ /^[0-9]+$/ then
    $divider = ARGV[0].to_i
    $rest = ARGV[1].to_i
  else
    usage
  end 
end

threads = Array.new
dumps_dir = File.join( $data_dir, 'cache', 'dumps', 'wikipedia' )
Dir["#{dumps_dir}/*"].each do |dump_lc_dir|
  lang = File.basename( dump_lc_dir )
  dump_dir = File.join( dump_lc_dir, 'current' )
  if File.directory?( dump_dir ) then
    db_dir = File.join( $data_dir, 'db', 'wikipedia', 'articles' )
    FileUtils.mkdir_p( db_dir )
    db_fpath = File.join( db_dir, "#{lang}.json" )
    articles_fpath = File.join( dump_dir, 'articles.tsv.gz' )
    geo_fpath = File.join( dump_dir, 'geo_tags.tsv.gz' )
    if File.exist?( articles_fpath ) then
      STDOUT.puts( "Processing #{lang}..." )
      thread = Thread.new{ process_language( db_fpath, articles_fpath, lang, geo_fpath ) }
      threads.push( thread )
    end
  end
end
threads.each{|t| t.join}

