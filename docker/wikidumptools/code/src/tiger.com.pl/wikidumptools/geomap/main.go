package geomap

import "image"
import "image/color"
import "image/png"
import "fmt"
import "sort"
import "os"
import "path"
import "strconv"
import utils "tiger.com.pl/wikidumptools/utils"

func dumpDoc() {
  fmt.Fprintf( os.Stdout, "%s\n", `` )
}

const ImageWidth = 2400

var DataDir string = "/db"
var LangDataDir string
var LangPublicDataDir string
var LangWiki string
var LangCode string

type GeoArticle struct {
  PageId string
  Lat float64
  Lon float64
  Len int
  Views int
}

var GeoArticlesById map[string]*GeoArticle = make(map[string]*GeoArticle)

func process_geo_tags_table( p map[string]string ) {
  if (p["gt_globe"] == "earth") && (p["gt_primary"] == "1") {
    gt_page_id := p["gt_page_id"]
    gt_lat, err := strconv.ParseFloat( p["gt_lat"], 64 )
    if err != nil {
      panic( err )
    }
    gt_lon, err := strconv.ParseFloat( p["gt_lon"], 64 )
    if err != nil {
      panic( err )
    }
    geo_article := GeoArticle {
      PageId: gt_page_id,
      Lat: gt_lat,
      Lon: gt_lon }
    GeoArticlesById[gt_page_id] = &geo_article
  }
}

func process_articles_table( p map[string]string ) {
  if geo_article, ok := GeoArticlesById[p["page_id"]]; ok {
    geo_article.Len, _ = strconv.Atoi(p["page_len"])
  }
}

func process_year_pageview_articles_table( p map[string]string ) {
  geo_article, ok := GeoArticlesById[p["page_id"]]
  if ok {
    geo_article.Views, _ = strconv.Atoi(p["views"])
  }
}

type ProcessGeoRecordFun func( *GeoArticle, string, map[string]int )

func articleCountFun( geo_article *GeoArticle, key string, hash map[string]int ) {
  hash[key] += 1
}

func lenCountFun( geo_article *GeoArticle, key string, hash map[string]int ) {
  hash[key] += geo_article.Len
}

func viewsCountFun( geo_article *GeoArticle, key string, hash map[string]int ) {
  hash[key] += geo_article.Views
}

func generateImage( width int, imageFPath string, fn ProcessGeoRecordFun ) {
  height := (int)(((float64)(width)) * 0.504684565)
  img := image.NewNRGBA(image.Rect(0, 0, width, height ))
  for x := 0; x < width; x++ {
    for y := 0; y < height; y++ {
      img.Set(x, y, color.Black)
    }
  }
  hash := make(map[string]int)
  for _, geo_article := range GeoArticlesById {
    x :=  (int) ((((float64)(width))/360.0) * (180 + geo_article.Lon));
    y :=  (int) ((((float64)(height))/180.0) * (90 - geo_article.Lat));
    key := fmt.Sprintf("%d %d", x, y )
    fn( geo_article, key, hash )
  }
  keys := make([]int, len(hash))
  for _, val := range hash {
    keys = append( keys, val )
  }
  sort.Ints(keys)
  var idx float64 = (float64) (len(keys)) * 0.95
  max := keys[(int)(idx)] // 95 percentyl
  // fmt.Fprintf( os.Stderr, "MAX: %d\n", max )
  col_min := 50
  col_max := 255
  for _, geo_article := range GeoArticlesById {
    x :=  (int) ((((float64)(width))/360.0) * (180 + geo_article.Lon));
    y :=  (int) ((((float64)(height))/180.0) * (90 - geo_article.Lat));
    key := fmt.Sprintf("%d %d", x, y )
    if hash[key] > max {
      hash[key] = max
    }
    val := col_min + (int) ( ((float64)(col_max - col_min)) * (((float64) (hash[key]))/((float64)(max))) )
    val8 := (uint8) (val)
    // fmt.Println( val8 )
    img.Set(x, y, color.NRGBA{ R: val8, G: val8, B: val8, A: 255 })
  }
  f, err := os.Create( imageFPath )
  if err != nil {
    panic(err)
  }
  if err := png.Encode(f, img); err != nil {
    f.Close()
    panic( err )
  }
  f.Sync()
  if err := f.Close(); err != nil {
    panic( err )
  }
}

func Main( args []string ) bool {
  if len(args) < 2 {
    return false
  }
  LangWiki = args[1]
  if LangWiki == "doc" {
    dumpDoc()
    return true
  }

  LangCode, LangDataDir = utils.ParseLangWiki( LangWiki, false )
  _, LangPublicDataDir = utils.ParsePublicLangWiki( LangWiki, true )

  utils.ProcessLocalFile( path.Join( LangDataDir, "geo_tags.tsv" ), process_geo_tags_table, "\t", nil );
  utils.ProcessLocalFile( path.Join( LangDataDir, "articles.tsv" ), process_articles_table, "\t", nil );
  utils.ProcessLocalFile( path.Join( LangDataDir, "months_pageview_articles.tsv" ), process_year_pageview_articles_table, "\t", nil );

  generateImage( ImageWidth, path.Join( LangPublicDataDir, "geo_by_pagecount.png" ), articleCountFun )
  generateImage( ImageWidth, path.Join( LangPublicDataDir, "geo_by_pagelen.png" ), lenCountFun )
  generateImage( ImageWidth, path.Join( LangPublicDataDir, "geo_by_pageviews.png" ), viewsCountFun )

  return true
}

