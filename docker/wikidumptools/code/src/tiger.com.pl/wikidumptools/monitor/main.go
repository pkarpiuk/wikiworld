package monitor

import "io"
import "io/ioutil"
import "flag"
import "fmt"
import "os"
import "path"
import "strconv"
import "strings"
import "time"
import "net/url"
import "net/http"
import "encoding/json"
import "compress/gzip"
import utils "tiger.com.pl/wikidumptools/utils"
import cathier "tiger.com.pl/wikidumptools/cathier"
import createts "tiger.com.pl/wikidumptools/createts"
import clickstream "tiger.com.pl/wikidumptools/clickstream"

type WikiArticle struct {
  PageId int64 `json:"page_id"`
  Title string `json:"title"`
  RedirTitles []string `json:"redir_titles"`
  Templates []string `json:"templates"`
  Len int `json:"len"`
  CreateTs string `json:"create_ts"`
  MonthViews []int `json:"month_views"`
  CatIds []int64 `json:"cat_ids"`
  DayViews []int `json:"day_views"`
  MonthAvgViews int `json:"month_avg_views"` // średnio miesięcznie w ostatnich 12-13 miesiącach (tam gdzie > 0)
  DayAvgViews int `json:"day_avg_views"` // średnio dziennie w ostatnich dniach (tam gdzie > 0)
  Prev map[string]int  `json:"prev,omitempty"` // clickstream
  Next map[string]int  `json:"next,omitempty"` // clickstream
}

type Info struct {
  PubDate string `json:"pub_date"`
  CategoriesCount int `json:"categories_count"`
  ArticlesCount int `json:"articles_count"`
  CategoriesFSize int64 `json:"categories_fsize"`
  CategoriesFSizeGz int64 `json:"categories_fsize_gz"`
  ArticlesFSize int64 `json:"articles_fsize"`
  ArticlesFSizeGz int64 `json:"articles_fsize_gz"`
}

type WikiRedirect struct {
  RedirectId string
  ArticleId int64
  RedirectTitle string
}

var ArticlesByPageId map[int64]*WikiArticle = make(map[int64]*WikiArticle)
var ArticlesByTitle map[string]*WikiArticle = make(map[string]*WikiArticle)
var RedirectsByTitle map[string]*WikiRedirect = make(map[string]*WikiRedirect)

var LangWiki string
var LangCode string
var LangCode2 string
var ProjectCode string
var ProjectCode2 string

var LangDataDir string
var LangPublicDataDir string
var PublicOutputDir string
var CatsByTitle map[string]*cathier.Category = make(map[string]*cathier.Category) // achtung: WSZYSTKIE kategorie
var CatsById map[int64]*cathier.Category = make(map[int64]*cathier.Category) // achtung: TYLKO kategorie z poddrzewa badanej kategorii wskazanej w wywołaniu programu

var WithSubcatsFlag = flag.Bool( "with-subcats", true, "With subcats (bool)" )
var RemoveCatsFlag = flag.String( "remove-cats", "", "Categories to remove: 'name-or-id|name-or-id...'" )
var TimespanFlag = flag.String( "timespan", "7", "'YYYY-MM-DD:YYYY-MM-DD' or 'integer'" )
var CatsOnlyFlag = flag.Bool( "cats-only", false, "Generate category tree only (categories.json.tgz)" )
var CategoryRootName string = ""

var CategoryRoot *cathier.Category
var RemoveCats map[int64]*cathier.Category = make(map[int64]*cathier.Category)
var StartDate time.Time
var EndDate time.Time
var CatsOnly bool

var MonthStartDate time.Time
var DaysCount int = 0
var DayIndex int = 0
var MonthsCount = 12
var MonthIndex = 0

func dumpDoc() {
  fmt.Fprintf( os.Stdout, "%s\n", `Usage: monitor plwiki [options] 'cat-name-or-id'` )
  fmt.Fprintf( os.Stdout, "%s\n", `  Options:` )
  flag.PrintDefaults()
}


// https://dumps.wikimedia.org/other/pageviews/2019/2019-05/pageviews-20190505-170000.gz
func urlForTime( t time.Time ) (*url.URL) {
  result, _ := url.Parse( fmt.Sprintf( "https://dumps.wikimedia.org/other/pageviews/%d/%d-%02d/pageviews-%d%02d%02d-%02d0000.gz", t.Year(), t.Year(), t.Month(), t.Year(), t.Month(), t.Day(), t.Hour() ) )
  return result
}

func DownloadFile(filepath string, url string) error {
  resp, err := http.Get(url)
  if err != nil {
    return err
  }
  defer resp.Body.Close()
  out, err := os.Create(filepath)
  if err != nil {
    return err
  }
  defer out.Close()
  _, err = io.Copy(out, resp.Body)
  return err
}

var DownloadsCount int = 0
func downloadFile( filepath string, url string ) error {
  fmt.Fprintf( os.Stderr, "Downloading '%s'... ", url )
  err := DownloadFile( filepath, url )
  fmt.Fprintf( os.Stderr, "DONE\n", url )
  if err != nil {
    fmt.Fprintf( os.Stderr, "%s\n", err )
  } else {
    DownloadsCount += 1
    // fmt.Fprintf( os.Stderr, "OK\n" )
  }
  return err
}

func findCategory( idOrTitle string ) *cathier.Category {
  id, err := strconv.ParseInt( idOrTitle, 10, 64 )
  if err == nil {
    result := cathier.CatsByPageId[id]
    if result != nil {
      return result
    }
  }
  return CatsByTitle[idOrTitle]
}

func LoadCategories( fpath string ) {
  fmt.Fprintf( os.Stderr, "Loading %s... ", fpath )
  var r io.ReadCloser
  // sprawdzamy czy jest wersja spakowana
  gfpath := fpath + ".gz"
  if _, err := os.Stat(gfpath); err == nil {
    fpath = gfpath
  }
  file, err := os.Open( fpath )
  if err != nil {
    panic( err )
  }
  defer file.Close()
  if fpath == gfpath {
    gz, err := gzip.NewReader(file)
    if err != nil {
      panic( err )
    }
    defer gz.Close()
    r = gz
  } else {
    r = file
  }
  cathier.LoadCategories( r )
  for _, cat := range cathier.CatsByPageId {
    // fmt.Fprintf( os.Stderr, "%s\n", cat.Title )
    CatsByTitle[cat.Title] = cat
  }
  fmt.Fprintf( os.Stderr, "DONE\n" )
}

func exist(path string) (bool) {
  _, err := os.Stat(path)
  if err == nil { return true }
  if os.IsNotExist(err) { return false }
  panic( err )
}

func fsize(path string) int64 {
  fi, err := os.Stat(path)
  if err != nil {
    panic( err )
  }
  return fi.Size()
}

func DownloadPageViewsDaily( startDate time.Time, endDate time.Time ) {
  for date := startDate; !date.After( endDate ); date = date.AddDate(0,0,1) {
    DaysCount += 1
    dir := path.Join( utils.DataDir, "pageview", fmt.Sprintf( "%d", date.Year() ), fmt.Sprintf( "%02d", date.Month() ) )
    os.MkdirAll( dir, 0777 )
    fname := fmt.Sprintf( "pagecounts-%d-%02d-%02d.bz2", date.Year(), date.Month(), date.Day() )
    fpath := path.Join( dir, fname )
    if !exist( fpath ) || fsize( fpath ) < 32768 {
      url := fmt.Sprintf( "https://dumps.wikimedia.org/other/pagecounts-ez/merged/%d/%d-%02d/%s", date.Year(), date.Year(), date.Month(), fname )
      downloadFile( fpath, url )
    }
  }
}

func VerifyYearPageViews() {
  MonthStartDate = time.Now().AddDate(0,-13,0)
  for month := 0; month <= 12; month++ {
    date := MonthStartDate.AddDate(0,month,0)
    dir := path.Join( utils.DataDir, "pageview", fmt.Sprintf( "%d", date.Year() ) )
    if exist( dir ) {
      fname := fmt.Sprintf( "pagecounts-%d-%02d-views-ge-5-totals.bz2", date.Year(), date.Month() )
      fpath := path.Join( dir, fname )
      if (month < 12) && !exist( fpath ) {
        fmt.Fprintf( os.Stderr, "There is no file '%s'. Please run wikidump year-pageview.\n", fpath )
        os.Exit( 1 )
      } else {
        if month == 12 {
          MonthsCount = 13
        }
      }
    } else {
      if month < 12 {
        fmt.Fprintf( os.Stderr, "There is no dir '%s'. Please run wikidump year-pageview.\n", dir )
        os.Exit( 1 )
      }
    }
  }
}

var ArticleCounter int = 0
func traverseCats( node *cathier.Category ) {
  if _, ok := CatsById[node.Id]; !ok {
    CatsById[node.Id] = node
    ArticleCounter += node.ArticlesCount
    if node.Children != nil {
      for _, cid := range node.Children {
        if _, ok := RemoveCats[cid]; !ok {
          traverseCats( cathier.CatsByPageId[cid] )
        }
      }
    }
  }
}

func ProcessCats( root *cathier.Category ) {
  fmt.Fprintf( os.Stderr, "Process cats [%s]... ", root.Title )
  traverseCats( root )
  fmt.Fprintf( os.Stderr, "DONE\n" )
  for _, cat := range CatsById {
    if cat.Parents != nil {
      newParents := make([]int64, 0)
      for _, parentId := range cat.Parents {
        if _, ok := CatsById[parentId]; ok {
          newParents = append(newParents, parentId)
        }
      }
      cat.Parents = newParents
    }
    if cat.Children != nil {
      newChildren := make([]int64, 0)
      for _, childId := range cat.Children {
        if _, ok := CatsById[childId]; ok {
          newChildren = append(newChildren, childId)
        }
      }
      cat.Children = newChildren
    }
    cat.SubtreeCatsCount = 0
    cat.SubtreeArticlesCount = 0
  }
  fmt.Fprintf( os.Stdout, "Categories in tree: %d\n", len(CatsById) )
  fmt.Fprintf( os.Stdout, "Max articles in tree: %d\n", ArticleCounter )
}

func monitorSubtreeFun( node *cathier.Category, accArticles map[int64]bool ) {
  sum := 0
  for pid, _ := range accArticles {
    article := ArticlesByPageId[pid]
    if article != nil {
      sum += article.DayAvgViews
    }
  }
  // node.Popularity = int(float64(sum)/float64(len(accArticles)))
  node.Popularity = sum
}

func ProcessCats2( root *cathier.Category, subtreeFun cathier.SubtreeFun ) {
  cathier.MakeLinks( CatsById )
  cathier.GlobalSubtreeArticles( root, CatsById, subtreeFun )
}

func saveCats() {
  file, err := os.Create( path.Join( PublicOutputDir, "categories.json.gz" ) )
  if err != nil {
    panic( err )
  }
  defer file.Close()
  gz := gzip.NewWriter( file )
  enc := json.NewEncoder( gz )
  enc.Encode( CatsById )
  gz.Flush()
  gz.Close()
}

func fileSize( fpath string ) int64 {
  fi, err := os.Stat(fpath);
  if err != nil {
    panic(err)
  }
  return fi.Size()
}

func unpackedFileSize( fpath string ) int64 {
  var fsize int64
  file, err := os.Open( fpath )
  if err != nil {
    panic( err )
  }
  defer file.Close()
  gz, err := gzip.NewReader(file)
  if err != nil {
    panic( err )
  }
  defer gz.Close()
  if fsize, err = io.Copy(ioutil.Discard, gz); err != nil {
    panic( err )
  }
  return fsize
}

func saveInfo() {
  var info Info
  info.PubDate = time.Now().Format("2006-01-02 15:04:05")
  info.CategoriesCount = len(CatsById)
  info.ArticlesCount = len(ArticlesByPageId)
  info.CategoriesFSizeGz = fileSize( path.Join( PublicOutputDir, "categories.json.gz" ) )
  info.CategoriesFSize = unpackedFileSize( path.Join( PublicOutputDir, "categories.json.gz" ) )
  info.ArticlesFSizeGz = fileSize( path.Join( PublicOutputDir, "articles.json.gz" ) )
  info.ArticlesFSize = unpackedFileSize( path.Join( PublicOutputDir, "articles.json.gz" ) )
  file, err := os.Create( path.Join( PublicOutputDir, "info.json" ) )
  if err != nil {
    panic( err )
  }
  defer file.Close()
  enc := json.NewEncoder( file )
  enc.Encode( &info )
}

func saveArticles() {
  file, err := os.Create( path.Join( PublicOutputDir, "articles.json.gz" ) )
  if err != nil {
    panic( err )
  }
  defer file.Close()
  gz := gzip.NewWriter( file )
  enc := json.NewEncoder( gz )
  enc.Encode( ArticlesByPageId )
  gz.Flush()
  gz.Close()
}

func mkdir( dir string ) {
  err := os.MkdirAll( dir, 0777 )
  if err != nil {
    fmt.Fprintf( os.Stderr, "Cannot create dir '%s'", dir )
    os.Exit( 1 )
  }
}

func process_article2category( p map[string]string ) {
  article_id := utils.Atoi64(p["cl_from"])
  cat_id := utils.Atoi64(p["cl_to"])
  if cat, ok := CatsById[cat_id]; ok {
    article, ok2 := ArticlesByPageId[article_id]
    if !ok2 {
      var record WikiArticle
      record.PageId = article_id
      record.CatIds = make([]int64,0)
      record.RedirTitles = make([]string,0)
      record.DayViews = make([]int, DaysCount)
      record.MonthViews = make([]int, MonthsCount)
      record.Templates = make([]string,0)
      ArticlesByPageId[article_id] = &record
      article = &record
    }
    article.CatIds = append(article.CatIds, cat_id)
    cat.AddLocalArticle( article_id )
  }
}

func process_articles( p map[string]string ) {
  page_id := utils.Atoi64(p["page_id"])
  article := ArticlesByPageId[page_id]
  if article != nil {
    t := createts.Process( page_id )
    if t == nil {
      return
    }
    article.Title = p["page_title"]
    article.Len, _ = strconv.Atoi(p["page_len"])
    // fmt.Fprintf( os.Stderr, "  -> %s %v\n", article.PageId, t )
    article.CreateTs = t.Format( "2006-01-02 15:04:05" )
  }
}

func process_redirects( p map[string]string ) {
  article := ArticlesByPageId[utils.Atoi64(p["article_id"])]
  if article != nil {
    redir_title := p["redir_title"]
    article.RedirTitles = append(article.RedirTitles, redir_title)
    redir := WikiRedirect {
      RedirectId: p["redir_id"],
      ArticleId: utils.Atoi64(p["article_id"]),
      RedirectTitle: redir_title }
    RedirectsByTitle[redir_title] = &redir
  }
}

func process_year_pageview( p map[string]string ) {
  wiki_code := p["wiki_code"]
  page_title := p["page_title"]
  monthly_total := p["monthly_total"]
  if strings.HasPrefix( wiki_code, LangCode2 ) && strings.HasSuffix( wiki_code, ProjectCode2 ) {
    article, ok := ArticlesByTitle[page_title]
    if !ok {
      redirect, ok2 := RedirectsByTitle[page_title]
      if ok2 {
        article = ArticlesByPageId[redirect.ArticleId]
      }
    }
    if article != nil {
      views, err := strconv.Atoi(monthly_total)
      if err != nil {
        panic( err )
      }
      article.MonthViews[MonthIndex] += views
    }
  }
}

func process_day_pageview( p map[string]string ) {
  wiki_code := p["wiki_code"]
  page_title := p["page_title"]
  daily_total := p["daily_total"]
  if strings.HasPrefix( wiki_code, LangCode2 ) && strings.HasSuffix( wiki_code, ProjectCode2 ) {
    article, ok := ArticlesByTitle[page_title]
    if !ok {
      redirect, ok2 := RedirectsByTitle[page_title]
      if ok2 {
        article = ArticlesByPageId[redirect.ArticleId]
      }
    }
    if article != nil {
      views, err := strconv.Atoi(daily_total)
      if err != nil {
        panic( err )
      }
      article.DayViews[DayIndex] += views
    }
  }
}

func process_clickstream( cart *clickstream.Article ) {
  article := ArticlesByPageId[int64(cart.PageId)]
  if article != nil {
    article.Prev = cart.Prev
    article.Next = cart.Next
  }
}

func process_templatelinks( p map[string]string ) {
  tl_from := utils.Atoi64(p["tl_from"])
  tl_namespace := p["tl_namespace"]
  if tl_namespace == "10" {
    article := ArticlesByPageId[tl_from]
    if article != nil {
      article.Templates = append(article.Templates, p["tl_title"])
    }
  }
}

func Main( args []string ) bool {
  if len(args) < 1 {
    return false
  }
  LangWiki = args[1]
  if LangWiki == "doc" {
    dumpDoc()
    return true
  }

  LangCode, LangDataDir = utils.ParseLangWiki( LangWiki, false )
  LangCode2 = LangCode + "."
  _, LangPublicDataDir = utils.ParsePublicLangWiki( LangWiki, true )
  os.Args = os.Args[2:]
  flag.Parse()
  if len(flag.Args()) != 1 {
    dumpDoc()
    os.Exit( 1 )
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

  CatsOnly = *CatsOnlyFlag
  dayCount, err := strconv.Atoi( *TimespanFlag )
  if err == nil {
    EndDate = time.Now().AddDate(0,0,-1)
    StartDate = EndDate.AddDate(0,0,-dayCount+1)
  } else {
    arr := strings.Split( *TimespanFlag, ":" )
    if len(arr) == 2 {
      StartDate, err = time.Parse( "2006-01-02", arr[0] )
      date, err2 := time.Parse( "2006-01-02", arr[1] )
      if (err != nil) || (err2 != nil) {
        fmt.Fprintf( os.Stderr, "Unknown timespan value: '%s'\n", *TimespanFlag )
        dumpDoc()
        os.Exit( 1 )
      }
      EndDate = date
    } else {
      fmt.Fprintf( os.Stderr, "Unknown timespan value: '%s'\n", *TimespanFlag )
      dumpDoc()
      os.Exit( 1 )
    }
  }
  if StartDate.Before( EndDate ) {
    if !CatsOnly {
      DownloadPageViewsDaily( StartDate, EndDate )
    }
  } else {
    fmt.Fprintf( os.Stderr, "Invalid timespan value: '%s'\n", *TimespanFlag )
    dumpDoc()
    os.Exit( 1 )
  }
  VerifyYearPageViews()

  fmt.Fprintf( os.Stderr, "DataDir: %s,\nLangDataDir: %s,\nLangPublicDataDir: %s\n", utils.DataDir, LangDataDir, LangPublicDataDir )
  fmt.Fprintf( os.Stderr, "%s to %s\n", StartDate.Format("2006-01-02"), EndDate.Format("2006-01-02" ) )

  CategoryRootName = strings.Replace(flag.Args()[0], " ", "_", -1)
  PublicOutputDir = path.Join( LangPublicDataDir, CategoryRootName )
  mkdir( PublicOutputDir )
  LoadCategories( path.Join( LangDataDir, "categories.json") )
  CategoryRoot = findCategory( strings.TrimSpace( CategoryRootName ) )
  if CategoryRoot == nil {
    fmt.Fprintf( os.Stderr, "Unknown category id or title: '%s'\n", CategoryRootName )
    os.Exit( 1 )
  }
  for _, str := range strings.Split( *RemoveCatsFlag, "|" ) {
    str = strings.Replace(strings.TrimSpace( str ), " ", "_", -1)
    if str != "" {
      cat := findCategory( str )
      if cat == nil {
        fmt.Fprintf( os.Stderr, "Unknown category id or title: '%s'\n", str )
        os.Exit( 1 )
      } else {
        RemoveCats[cat.Id] = cat
      }
    }
  }
  ProcessCats( CategoryRoot )

  createts.Begin( LangWiki )
  defer createts.End()

  utils.ProcessLocalFile( path.Join( LangDataDir, "article2category.tsv" ), process_article2category, "\t", nil );
  if CatsOnly {
    ProcessCats2( CategoryRoot, nil )
    saveCats()
    return true
  }
  utils.ProcessLocalFile( path.Join( LangDataDir, "articles.tsv" ), process_articles, "\t", nil );
  utils.ProcessLocalFile( path.Join( LangDataDir, "redirects.tsv" ), process_redirects, "\t", nil );
  utils.ProcessLocalFile( path.Join( LangDataDir, "templatelinks.tsv" ), process_templatelinks, "\t", nil );

  idsToRemove := make([]int64,0)
  for pid, article := range ArticlesByPageId {
    if (article == nil) || (article.Title == "") {
      idsToRemove = append(idsToRemove, pid)
    } else {
      ArticlesByTitle[article.Title] = article
    }
  }
  for _, pid := range idsToRemove {
    delete( ArticlesByPageId, pid )
  }

  paramHash := map[string]int { "wiki_code": 0, "page_title": 1, "daily_total": 2, "hour_views": 3 }
  for date := StartDate; !date.After(EndDate); date = date.AddDate(0,0,1) {
    dir := path.Join( utils.DataDir, "pageview", fmt.Sprintf( "%d", date.Year() ), fmt.Sprintf( "%02d", date.Month() ) )
    fname := fmt.Sprintf( "pagecounts-%d-%02d-%02d", date.Year(), date.Month(), date.Day() )
    fpath := path.Join( dir, fname )
    utils.ProcessLocalFile( fpath, process_day_pageview, " ", paramHash )
    DayIndex += 1
  }

  paramHash = map[string]int { "wiki_code": 0, "page_title": 1, "monthly_total": 2 }
  MonthStartDate = time.Now().AddDate(0,-13,0)
  for month := 0; month <= 12; month++ {
    date := MonthStartDate.AddDate(0,month,0)
    dir := path.Join( utils.DataDir, "pageview", fmt.Sprintf( "%d", date.Year() ) )
    fname := fmt.Sprintf( "pagecounts-%d-%02d-views-ge-5-totals.bz2", date.Year(), date.Month() )
    fpath := strings.Replace( path.Join( dir, fname ), ".bz2", "", -1 )
    if exist( fpath + ".bz2" ) {
      utils.ProcessLocalFile( fpath, process_year_pageview, " ", paramHash )
    }
    MonthIndex += 1
  }

  for _, article := range ArticlesByPageId {
    count := 0
    sum := 0
    for _, val := range article.MonthViews {
      if val > 0 {
        count += 1
        sum += val
      }
    }
    if count > 0 {
      article.MonthAvgViews = int(float64(sum)/float64(count))
    }
    count = 0
    sum = 0
    for _, val := range article.DayViews {
      if val > 0 {
        count += 1
        sum += val
      }
    }
    if count > 0 {
      article.DayAvgViews = int(float64(sum)/float64(count))
    }
  }

  cspath := path.Join( LangDataDir, "clickstream-articles.json.gz" )
  if exist( cspath ) {
    clickstream.LoadClickstream( path.Join( LangDataDir, "clickstream-articles.json" ), process_clickstream )
  }
  ProcessCats2( CategoryRoot, monitorSubtreeFun )

  saveCats()
  saveArticles()
  saveInfo()
  return true
}

