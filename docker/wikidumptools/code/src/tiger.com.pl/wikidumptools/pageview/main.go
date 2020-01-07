package pageview

import "fmt"
import "os"
import "path"
import "strconv"
import "io/ioutil"
import "strings"
import "sort"
import utils "tiger.com.pl/wikidumptools/utils"

func dumpDoc() {
  fmt.Fprintf( os.Stdout, "%s\n", `` )
}

var DataDir string = "/db"
var LangDataDir string
var LangWiki string
var LangCode string
var LangCode2 string
var ProjectCode string
var ProjectCode2 string

type Redirect struct {
  RedirectId string
  ArticleId string
  RedirectTitle string
}

type Article struct {
  PageId string
  Title string
  Views int
}

var RedirectsByTitle map[string]*Redirect = make(map[string]*Redirect)
var ArticlesById map[string]*Article = make(map[string]*Article)
var ArticlesByTitle map[string]*Article = make(map[string]*Article)

func process_redirects_table( p map[string]string ) {
  redir_title := p["redir_title"]
  redir := Redirect {
    RedirectId: p["redir_id"],
    ArticleId: p["article_id"],
    RedirectTitle: redir_title }
  RedirectsByTitle[redir_title] = &redir
}

func process_articles_table( p map[string]string ) {
  page_id := p["page_id"]
  page_title := p["page_title"]
  article := Article {
    PageId: page_id,
    Title: page_title,
    Views: 0 }
  ArticlesById[page_id] = &article
  ArticlesByTitle[page_title] = &article
}

func process_pageview( p map[string]string ) {
  wiki_code := p["wiki_code"]
  page_title := p["page_title"]
  monthly_total := p["monthly_total"]
  if strings.HasPrefix( wiki_code, LangCode2 ) && strings.HasSuffix( wiki_code, ProjectCode2 ) {
    article, ok := ArticlesByTitle[page_title]
    if !ok {
      redirect, ok2 := RedirectsByTitle[page_title]
      if ok2 {
        article = ArticlesById[redirect.ArticleId]
      }
    }
    if article != nil {
      views, err := strconv.Atoi(monthly_total)
      if err != nil {
        panic( err )
      }
      article.Views += views
    }
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
  switch LangWiki[strings.Index(LangWiki,"wiki"):] {
    case "wikibooks":
      ProjectCode = "b"
    case "wikinews":
      ProjectCode = "n"
    case "wikivoyage":
      ProjectCode = "o"
    case "wikiquote":
      ProjectCode = "q"
    case "wikisource":
      ProjectCode = "s"
    case "wikiversity":
      ProjectCode = "v"
    case "wiki":
      ProjectCode = "z"
    default:
      ProjectCode = "m"
  }
  ProjectCode2 = "." + ProjectCode
  LangCode, LangDataDir = utils.ParseLangWiki( LangWiki, false )
  LangCode2 = LangCode + "."

  utils.ProcessLocalFile( path.Join( LangDataDir, "redirects.tsv" ), process_redirects_table, "\t", nil );
  utils.ProcessLocalFile( path.Join( LangDataDir, "articles.tsv" ), process_articles_table, "\t", nil );

  monthsCount := 12
  if len(args) > 2 {
    m, err := strconv.Atoi( args[2] )
    if err != nil {
      panic( err )
    }
    monthsCount = m
  }
  statsDir := path.Join( utils.DataDir, "cache", "pageview", "months" )
  if _, err := os.Stat(statsDir); os.IsNotExist(err) {
    panic( "There is no directory' " + statsDir + "'" )
  }
  files, err := ioutil.ReadDir(statsDir)
  if err != nil {
    panic( err )
  }
  bz2s := make([]os.FileInfo, 0)
  for _, f := range files {
    if strings.HasSuffix( f.Name(), ".bz2" ) {
      bz2s = append( bz2s, f )
    }
  }
  if len(bz2s) < monthsCount {
    panic( fmt.Sprintf( "Too few data files in %s (required %d, found %d)", statsDir, monthsCount, len(bz2s) ) )
  }
  sort.Slice( bz2s, func( i, j int ) bool {
    return bz2s[i].Name() >= bz2s[j].Name()
  })
  bz2s = bz2s[0:monthsCount]
  paramHash := map[string]int { "wiki_code": 0, "page_title": 1, "monthly_total": 2 }
  for _, f := range bz2s {
    fname := strings.Replace( f.Name(), ".bz2", "", -1 )
    utils.ProcessLocalFile( path.Join( statsDir, fname ), process_pageview, " ", paramHash )
  }

  // generujemy plik wynikowy
  outFile, gzStream := utils.CreateGzFile( path.Join( LangDataDir, "months_pageview_articles.tsv.gz" ) )
  _, err = fmt.Fprintf( gzStream, "page_id\tpage_title\tviews\n" )
  if err != nil {
    panic( err )
  }
  for _, article := range ArticlesById {
    _, err := fmt.Fprintf( gzStream, "%s\t%s\t%d\n", article.PageId, article.Title, article.Views )
    if err != nil {
      panic( err )
    }
  }
  gzStream.Flush()
  gzStream.Close()
  outFile.Close()

  return true
}

