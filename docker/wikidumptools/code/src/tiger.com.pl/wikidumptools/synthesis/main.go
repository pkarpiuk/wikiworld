package synthesis

import "fmt"
import "regexp"
import "io"
import "bufio"
import "strings"
import "os"
import "path"
import "encoding/json"
import "compress/gzip"
import "strconv"
import "runtime"
import utils "tiger.com.pl/wikidumptools/utils"
import cathier "tiger.com.pl/wikidumptools/cathier"
import clickstream "tiger.com.pl/wikidumptools/clickstream"

var GenerateCategoriesFlag bool = true
var DisableFlags map[string]bool = make(map[string]bool) // tutaj wskazujemy, ktorych pol JSON struktury Article NIE generowac

type Gps struct {
  Lat float64    `json:"lat"`
  Lon float64    `json:"lon"`
  Primary bool   `json:"primary"`
  Globe string   `json:"globe,omitempty"`
  Dim int        `json:"dim,omitempty"`
  Type string    `json:"type,omitempty"`
  Name string    `json:"name,omitempty"`
  Country string `json:"country,omitempty"`
}

type Extra struct {
  IsNew bool `json:"is_new"`
  Touched string `json:"touched,omitempty"`
  LinksUpdated string `json:"links_updated,omitempty"`
  Latest string `json:"latest,omitempty"`
}

type PageRestrictions struct {
  Type string    `json:"type"`
  Level string   `json:"level"`
  Cascade string `json:"cascade"`
  User string    `json:"user"`
  Expiry string  `json:"expiry"`
}

type Article struct {
  PageId int64                `json:"page_id"`
  Title string                `json:"title"`
  Extra *Extra                `json:"extra,omitempty"`
  Gps *Gps                    `json:"gps,omitempty"`
  Length int                  `json:"length"`
  DisplayTitle string         `json:"displaytitle,omitempty"`
  WikiDataId string           `json:"wikidata_id,omitempty"`
  Extract string              `json:"extract,omitempty"`
  LangLinks map[string]string `json:"langlinks,omitempty"` // lang_code -> page_title; page_title może być tytułem artykułu lub przekierowania
  Redirects map[string]string `json:"redirects,omitempty"` // page_id -> title
  ImageFile string            `json:"image_file,omitempty"`
  Cats []int64                `json:"cats,omitempty"`
  PageLinks []string          `json:"pagelinks,omitempty"`
  Templates []string          `json:"templates,omitempty"` // namespace 10, min. infoboksy
  Modules []string            `json:"modules,omitempty"`   // namespace 828, Lua
  ExternalLinks []string      `json:"externallinks,omitempty"`
  ImageLinks []string         `json:"imagelinks,omitempty"`
  Views []int                 `json:"views,omitempty"`
  PageRestrictions []PageRestrictions `json:"page_restrictions,omitempty"`
  Prev map[string]int         `json:"prev,omitempty"`
  Next map[string]int         `json:"next,omitempty"`
  tmpHash map[string]map[string]bool
}

func (a *Article) toCache( cacheName string, value string ) {
  if a.tmpHash[cacheName] == nil {
    a.tmpHash[cacheName] = make(map[string]bool)
  }
  a.tmpHash[cacheName][value] = true
}

var AllArticlesById map[int64]*Article = make(map[int64]*Article)
var AllRedirectsById map[string]*Article = make(map[string]*Article)
var CategoryGraph map[int64]*cathier.Category = make(map[int64]*cathier.Category)

func CategoryGraphToSlice() []*cathier.Category {
  if GenerateCategoriesFlag {
    result := make([]*cathier.Category,0,len(CategoryGraph))
    return cathier.Accumulate( RootNode, result, make(map[int64]bool) )
  } else {
    return nil
  }
}

func readArticleIds( r io.ReadCloser ) {
  defer r.Close()
  scanner := bufio.NewScanner(r)
  for scanner.Scan() {
    line := scanner.Text()
    articleId := utils.Atoi64(strings.TrimSpace( line ))
    AllArticlesById[articleId] = nil
  }
}

func SetArticleIds( page_ids []int64 ) {
  runtime.GC()

  AllArticlesById = make(map[int64]*Article)
  AllRedirectsById = make(map[string]*Article)
  CategoryGraph = make(map[int64]*cathier.Category)
  RootNode = nil

  for _, articleId := range page_ids {
    AllArticlesById[articleId] = nil
  }
}

func process_articles_table( p map[string]string ) {
  page_id := utils.Atoi64(p["page_id"])
  if _, ok := AllArticlesById[page_id]; ok {
    length, _ := strconv.Atoi( p["page_len"] )
    article := Article {
      PageId: page_id,
      Title: p["page_title"],
      Redirects: make(map[string]string),
      Cats: make([]int64, 0),
      LangLinks: make(map[string]string),
      PageLinks: make([]string, 0),
      Templates: make([]string, 0),
      Modules: make([]string, 0),
      PageRestrictions: make([]PageRestrictions, 0),
      tmpHash: make(map[string]map[string]bool),
      Length: length }
    if !DisableFlags["extra"] {
      extra := Extra {
        IsNew: p["page_is_new"] == "1",
        Touched: p["page_touched"],
        LinksUpdated: p["page_links_updated"],
        Latest: p["page_latest"] }
      article.Extra = &extra
    }
    AllArticlesById[page_id] = &article
  }
}

func process_page_restrictions_table( p map[string]string ) {
  article := AllArticlesById[utils.Atoi64(p["pr_page"])]
  if article != nil {
    pageRestrictions := PageRestrictions {
      Type: p["pr_type"],
      Level: p["pr_level"],
      Cascade: p["pr_cascade"],
      User: p["pr_user"],
      Expiry: p["pr_expiry"] }
    article.PageRestrictions = append( article.PageRestrictions, pageRestrictions )
  }
}

func process_redirects_table( p map[string]string ) {
  article := AllArticlesById[utils.Atoi64(p["article_id"])]
  if article != nil {
    redir_id := p["redir_id"]
    AllRedirectsById[redir_id] = article
    if !DisableFlags["redirects"] {
      article.Redirects[redir_id] = p["redir_title"]
    }
  }
}

func process_langlinks_articles_table( p map[string]string ) {
  article := AllArticlesById[utils.Atoi64(p["ll_from"])]
  if article != nil {
    ll_lang := p["ll_lang"]
    if utils.SupportedLanguages[ll_lang] {
      article.LangLinks[ll_lang] = p["ll_title"]
    }
  }
}

func process_langlinks_redirects_table( p map[string]string ) {
  article := AllRedirectsById[p["ll_from"]]
  if article != nil {
    ll_lang := p["ll_lang"]
    if utils.SupportedLanguages[ll_lang] {
      article.LangLinks[ll_lang] = p["ll_title"]
    }
  }
}

func process_page_props_articles_table( p map[string]string ) {
  pp_page := utils.Atoi64(p["pp_page"])
  pp_propname := p["pp_propname"]
  if (pp_propname == "wikibase_item") || (pp_propname == "page_image_free") || (pp_propname == "displaytitle") {
    article := AllArticlesById[pp_page]
    if article != nil {
      pp_value := p["pp_value"]
      if (pp_propname == "wikibase_item") && !DisableFlags["wikidata_id"] {
        article.WikiDataId = pp_value
      } else if (pp_propname == "page_image_free") && !DisableFlags["image_file"] {
        article.ImageFile = pp_value
      } else if (pp_propname == "displaytitle") && !DisableFlags["displaytitle"] {
        article.DisplayTitle = pp_value
      }
    }
  }
}

func process_abstracts_table( p map[string]string ) {
  page_id := utils.Atoi64(p["page_id"])
  article := AllArticlesById[page_id]
  if article != nil {
    abstract := p["abstract"]
    if !BadAbstractRegexp.MatchString( abstract ) {
      article.Extract = abstract
    }
  }
}

func process_pagelinks_table( p map[string]string ) {
  article := AllArticlesById[utils.Atoi64(p["pl_from"])]
  if article != nil {
    to_page_id := p["to_page_id"]
    if AllArticlesById[utils.Atoi64(to_page_id)] != nil {
      article.toCache( "pagelinks", to_page_id )
    }
  }
}

func process_externallinks_table( p map[string]string ) {
  article := AllArticlesById[utils.Atoi64(p["el_from"])]
  if article != nil {
    article.toCache( "externallinks", p["el_to"] )
  }
}

func process_imagelinks_table( p map[string]string ) {
  article := AllArticlesById[utils.Atoi64(p["il_from"])]
  if article != nil {
    il_to := p["il_to"]
    il_to_lc := strings.ToLower( il_to )
    if strings.HasSuffix( il_to_lc, ".jpg" ) || strings.HasSuffix( il_to_lc, ".jpeg" ) {
      article.toCache( "imagelinks", il_to )
    }
  }
}

func process_templatelinks_table( p map[string]string ) {
  article := AllArticlesById[utils.Atoi64(p["tl_from"])]
  if article != nil {
    tl_namespace := p["tl_namespace"]
    tl_title := p["tl_title"]
    if tl_namespace == "10" {
      article.toCache( "templates", tl_title )
    } else if (tl_namespace == "828") && !DisableFlags["modules"] {
      article.toCache( "modules", tl_title )
    }
  }
}

func str2float( str string ) float64 {
  res, err := strconv.ParseFloat(str, 64)
  if err != nil {
    res = 0.0
  }
  return res
}

func process_geo_tags_table( p map[string]string ) {
  page_id := utils.Atoi64(p["gt_page_id"])
  article := AllArticlesById[page_id]
  if article != nil {
    dim_str := p["gt_dim"]
    if dim_str == "" {
      dim_str = "0"
    }
    dim, _ := strconv.Atoi( dim_str )
    article.Gps = &Gps {
      Lat: str2float( p["gt_lat"] ),
      Lon: str2float( p["gt_lon"] ),
      Primary: (p["gt_primary"] == "1"),
      Globe: p["gt_globe"],
      Dim: dim,
      Type: p["gt_type"],
      Name: p["gt_name"],
      Country: p["gt_country"] }
  }
}

func process_article2category_table( p map[string]string ) {
  cl_from := utils.Atoi64(p["cl_from"])
  article := AllArticlesById[cl_from]
  if article != nil {
    cl_to := utils.Atoi64(p["cl_to"])
    cat := cathier.CatsByPageId[cl_to]
    if cat != nil {
      CategoryGraph[cl_to] = cat
      article.toCache( "cats", p["cl_to"] )
    }
  }
}

func process_langlinks_categories_table( p map[string]string ) {
  if cat, ok := CategoryGraph[utils.Atoi64(p["ll_from"])]; ok {
    ll_lang := p["ll_lang"]
    if utils.SupportedLanguages[ll_lang] {
      cat.LangLinks[ll_lang] = p["ll_title"]
    }
  }
}

func process_year_pageview_table( p map[string]string ) {
  article := AllArticlesById[utils.Atoi64(p["page_id"])]
  if article != nil {
    views, _ := strconv.Atoi( p["views"] )
    article.Views = []int {views}
  }
}

func removeInaccessible( m map[string]int ) map[string]int {
  toRemove := []string {}
  for page_id, _ := range m {
    if !strings.HasPrefix( page_id, "other-" ) {
      article := AllArticlesById[utils.Atoi64(page_id)]
      if article == nil {
        toRemove = append( toRemove, page_id )
      }
    }
  }
  for _, page_id := range toRemove {
    delete( m, page_id )
  }
  return m
}

func process_clickstream( c_article *clickstream.Article ) {
  // fmt.Fprintf( os.Stderr, "   %d\n", c_article.PageId )
  article := AllArticlesById[int64(c_article.PageId)]
  if article != nil {
    article.Prev = removeInaccessible( c_article.Prev )
    article.Next = removeInaccessible( c_article.Next )
  }
}

func dump(w io.WriteCloser) {
  defer w.Close()
  enc := json.NewEncoder( w )
  for _, article := range AllArticlesById {
    if article != nil {
      enc.Encode(article)
    }
  }
  if GenerateCategoriesFlag {
    cathier.DumpTraverse( RootNode, enc )
  }
}

func LoadCategories( fpath string ) {
  fmt.Fprintf( os.Stderr, "Loading %s...\n", fpath )
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
}

var RootNode *cathier.Category

func buildCategoryGraph() {
  for {
    changeCounter := 0
    for _, cat := range CategoryGraph {
      if len(cat.Parents) == 0 {
        RootNode = cat
        fmt.Fprintf( os.Stderr, "ROOT NODE: '%s' (%s)\n", RootNode.Title, RootNode.Id )
      } else {
        for _, pid := range cat.Parents {
          if CategoryGraph[pid] == nil {
            changeCounter += 1
            parent := cathier.CatsByPageId[pid]
            if parent != nil {
              CategoryGraph[pid] = parent
            } else {
              panic( "Should not be nil" )
            }
          }
        }
      }
    }
    if changeCounter == 0 {
      break
    }
  }
  for _, cat := range CategoryGraph {
    cat.Init()
    cat.LangLinks = make(map[string]string)
  }
  cathier.MakeLinks( CategoryGraph )
  for _, article := range AllArticlesById {
    if article != nil {
      for _, pid := range article.Cats {
        cat := CategoryGraph[pid]
        if cat == nil {
          panic( "Should not be nil" )
        }
        cat.AddLocalArticle(article.PageId)
      }
    }
  }
  cathier.ComputeGraphProps( RootNode, CategoryGraph, true )
}

func dumpDoc() {
  fmt.Fprintf( os.Stdout, "%s\n", `Format rekordu artykułu:
  page_id: String
  title: String
  extra: patrz https://www.mediawiki.org/wiki/Manual:Page_table
    is_new: bool
    touched: String
    links_updated: String
    latest: String
  [gps]: patrz https://www.mediawiki.org/wiki/Extension:GeoData
    lat: float
    lon: float
    primary: bool
    globe: bool
    dim: bool
    type: String
    name: String
    country: String
  length: Int; długość artykułu w bajtach
  [displaytitle]: String
  [wikidata_id]: String
  [extract]: String; jednozdaniowy skrót artykułu
  [langlinks]: String->String; lang_code -> page_title; page_title może być tytułem artykułu lub przekierowania (uwaga: spacje nie są zamieniane na znaki podkreślenia)
  [redirects]: String->String; page_id -> page_title
  [image_file]: String; nazwa pliku obrazka głównego artykułu
  [cats]: String[] identyfikatory kategorii, do których należy artykuł
  [pagelinks]: String[] identyfikatory artykułów, do których linkuje artykuł
  [templates]: String[]; namespace 10, m.in. infoboksy
  [modules]: String[]; namespace 828, Lua
  [externallinks]: String[]; URLe do serwisów zewnętrznych
  [imagelinks]: String[]; nazwy plików obrazków osadzonych w artykule
  views: Int[]; tablica jednoelementowa: liczba odwiedzin w ubiegłym (lub 2018) roku
  [page_restrictions]: patrz https://www.mediawiki.org/wiki/Manual:Page_restrictions_table
    type: String
    level: String
    cascade: String
    user: String
    expiry: String
  [prev]: String->Int; page_title -> count, analogicznie jak w wyniku polecenia 'clickstream'
  [next]: String->Int; page_title -> count, analogicznie jak w wyniku polecenia 'clickstream'

Kategorie są generowane w formacie takim samym jak dla polecenia 'cathier'` )
}

var LangDataDir string
var LangWiki string
var LangCode string

var BadAbstractRegexp *regexp.Regexp

func Generate( langWiki string, w io.WriteCloser ) {
  LangWiki = langWiki
  re, err := regexp.Compile("[][|={}]")
  if err != nil {
    panic( err )
  }
  BadAbstractRegexp = re
  LangCode, LangDataDir = utils.ParseLangWiki( LangWiki, false )

  if GenerateCategoriesFlag {
    LoadCategories( path.Join( LangDataDir, "categories.json") )
  }
  utils.ProcessLocalFile( path.Join( LangDataDir, "articles.tsv"), process_articles_table, "\t", nil )
  utils.ProcessLocalFile( path.Join( LangDataDir, "geo_tags.tsv"), process_geo_tags_table, "\t", nil )
  if !DisableFlags["page_restrictions"] {
    utils.ProcessLocalFile( path.Join( LangDataDir, "page_restrictions.tsv"), process_page_restrictions_table, "\t", nil )
  }
  utils.ProcessLocalFile( path.Join( LangDataDir, "page_props_articles.tsv"), process_page_props_articles_table, "\t", nil )
  if !DisableFlags["extract"] {
    utils.ProcessLocalFile( path.Join( LangDataDir, "abstracts.tsv"), process_abstracts_table, "\t", nil )
  }
  if GenerateCategoriesFlag {
    utils.ProcessLocalFile( path.Join( LangDataDir, "article2category.tsv"), process_article2category_table, "\t", nil )
  }
  utils.ProcessLocalFile( path.Join( LangDataDir, "redirects.tsv"), process_redirects_table, "\t", nil )
  utils.ProcessLocalFile( path.Join( LangDataDir, "langlinks_articles.tsv"), process_langlinks_articles_table, "\t", nil )
  utils.ProcessLocalFile( path.Join( LangDataDir, "langlinks_redirects.tsv"), process_langlinks_redirects_table, "\t", nil )
  utils.ProcessLocalFile( path.Join( LangDataDir, "templatelinks.tsv"), process_templatelinks_table, "\t", nil )
  if !DisableFlags["externallinks"] {
    utils.ProcessLocalFile( path.Join( LangDataDir, "externallinks.tsv"), process_externallinks_table, "\t", nil )
  }
  if !DisableFlags["imagelinks"] {
    utils.ProcessLocalFile( path.Join( LangDataDir, "imagelinks.tsv"), process_imagelinks_table, "\t", nil )
  }
  utils.ProcessLocalFile( path.Join( LangDataDir, "pagelinks.tsv"), process_pagelinks_table, "\t", nil )
  if !DisableFlags["views"] {
    utils.ProcessLocalFile( path.Join( LangDataDir, "months_pageview_articles.tsv"), process_year_pageview_table, "\t", nil )
  }
  clickstreamFPath := path.Join( LangDataDir, "clickstream-articles.json" )
  if _, err := os.Stat(clickstreamFPath + ".gz"); err == nil {
    clickstream.LoadClickstream( clickstreamFPath, process_clickstream )
  }

  for _, article := range AllArticlesById {
    if article != nil {
      for cacheName, hash := range article.tmpHash {
        for value, _ := range hash {
          switch cacheName {
          case "cats":
            article.Cats = append( article.Cats, utils.Atoi64(value) )
          case "pagelinks":
            article.PageLinks = append( article.PageLinks, value )
          case "templates":
            article.Templates = append( article.Templates, value )
          case "modules":
            article.Modules = append( article.Modules, value )
          case "externallinks":
            article.ExternalLinks = append( article.ExternalLinks, value )
          case "imagelinks":
            article.ImageLinks = append( article.ImageLinks, value )
          }
        }
      }
    }
  }

  if GenerateCategoriesFlag {
    buildCategoryGraph()
    utils.ProcessLocalFile( path.Join( LangDataDir, "langlinks_categories.tsv"), process_langlinks_categories_table, "\t", nil )
  }

  if w != nil {
    dump( w )
  }

  if GenerateCategoriesFlag {
    cathier.Stats( CategoryGraph, true )
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

  readArticleIds( os.Stdin )

  Generate( LangWiki, os.Stdout )

  return true
}

