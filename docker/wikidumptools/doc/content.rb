#!/usr/bin/ruby

# Skrypt dostawia spis treści do strony HTML przekazanej na STDIN

require 'nokogiri'
require 'open-uri'

doc = Nokogiri::HTML( ARGF.read )
headers = []
minLevel = 10
minLevelCount = 0
doc.traverse do |node|
  if node.name =~ /^h\d+$/ then
    if node.name[1].to_i < minLevel then
      minLevel = node.name[1].to_i
      minLevelCount = 1
    elsif node.name[1].to_i == minLevel then
      minLevelCount += 1
    end
    headers.push( node )
  end
end

if minLevelCount == 1 then minLevel += 1 end

content_html = '<div style="position: fixed; right: 10px; top: 10px; bottom: 10px; padding: 8px; margin: 8px; font-size: 90%; background-color: lightgray; border-style: solid; border-width: 2px; border-color: black;">' + "\n"
breadcrumbs = [0]
headers.each do |node|
  level = node.name[1].to_i - minLevel
  if level >= 0 then
    while breadcrumbs.length-1 < level do
      breadcrumbs.push( 0 )
    end
    while breadcrumbs.length-1 > level do
      breadcrumbs.pop
    end
    breadcrumbs[level] += 1
    # anchor = URI::encode( breadcrumbs.join('.') + node.text.strip )
    anchor = URI::encode( node.text.strip )
    content_html += "#{'&nbsp;&nbsp;&nbsp;&nbsp;'*level}#{breadcrumbs.join('.')}. <a href=\"##{anchor}\">#{node.text.strip}</a><br/>\n"
    if node.parent.name != 'a' then
      node.replace('<a name="' + anchor + '"></a><' + node.name + '>' + "#{breadcrumbs.join('.')}. " + node.inner_html + '</' + node.name + '>')
    else
      node.parent['name'] = anchor
    end
  end
end
content_html += "</div>"
body = doc.css('body').first
body.inner_html = "#{content_html}\n#{body.inner_html}"

puts doc
