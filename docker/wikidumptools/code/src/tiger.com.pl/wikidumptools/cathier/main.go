package cathier

import "io"
import "fmt"
import "os"
import "strings"
import "compress/gzip"
import "encoding/json"
import "sort"
import "path"
import "runtime"
import "sync"
import "time"
import utils "tiger.com.pl/wikidumptools/utils"

type Category struct {
  Id int64 `json:"id"`
  Title string `json:"title"`
  Parents []int64 `json:"parents"`
  Children []int64 `json:"children"`
  WikiDataId string `json:"wikidata_id,omitempty"`
  IsMeta bool `json:"is_meta,omitempty"`
  ArticlesCount int `json:"articles_count,omitempty"` // liczba artykułów lokalnych
  SubtreeArticlesCount int `json:"subtree_articles_count,omitempty"`
  SubtreeCatsCount int `json:"subtree_cats_count,omitempty"`
  LangLinks map[string]string `json:"langlinks,omitempty"` // lang_code -> 'Kategoria:Miś Koralgol'; wykorzystywane tylko przez pakiet 'tiger.com.pl/wikidumptools/synthesis'
  Popularity int `json:"popularity,omitempty"`
  isHidden bool
  parents map[int64]*Category
  children map[int64]*Category
  localArticles map[int64]bool
}

// wykorzystywane przez pakiet 'tiger.com.pl/wikidumptools/synthesis'
func (c *Category) Init() {
  c.ArticlesCount = 0
  c.SubtreeArticlesCount = 0
  c.SubtreeCatsCount = 0
  c.parents = make(map[int64]*Category)
  c.children = make(map[int64]*Category)
  c.localArticles = make(map[int64]bool)
}

// wykorzystywane przez pakiet 'tiger.com.pl/wikidumptools/synthesis'
func (c *Category) AddLocalArticle( pageId int64 ) {
  c.localArticles[pageId] = true
}

type Worker struct {
  Id int
  Marks map[int64]bool
  AccArticles map[int64]bool
}

// wykorzystywane przez pakiet 'tiger.com.pl/wikidumptools/synthesis'
func MakeLinks(graph map[int64]*Category) {
  for _, cat := range graph {
    for _, pid := range cat.Parents {
      parent := graph[pid]
      if parent == nil {
        panic( "Should not be nil")
      }
      cat.parents[pid] = parent
      parent.children[cat.Id] = cat
    }
  }
}

var CatsByPageId map[int64]*Category = make(map[int64]*Category)
var RootNode *Category
var MetaRootNode *Category
var LangWiki string
var LangCode string

var Marks map[int64]bool = nil
var AccessibleNodes map[int64]*Category = make(map[int64]*Category)
var MetaNodes map[int64]*Category

func LoadCategories( r io.ReadCloser ) {
  dec := json.NewDecoder( r )
  for {
    var cat Category
    if err := dec.Decode(&cat); err == io.EOF {
			break
		} else if err != nil {
      panic( err )
		}
    cat.localArticles = make(map[int64]bool)
    cat.parents = make(map[int64]*Category)
    cat.children = make(map[int64]*Category)
    CatsByPageId[cat.Id] = &cat
  }
}

func dumpDoc() {
  fmt.Fprintf( os.Stdout, "W wynikowym pliku $DATA_DIR/<lang_code>/categories.json.gz każda kategoria to osobny wiersz (JSON) o polach:\n" )
  fmt.Fprintf( os.Stdout, "  id (String): identyfikator kategorii (tabela page w Wikipedii)\n" )
  fmt.Fprintf( os.Stdout, "  title (String): tytuł kategorii (bez prefiksu typu 'Kategoria:', spacje zamienione na znaki podkreślenia)\n" )
  fmt.Fprintf( os.Stdout, "  parents ([String]): tablica identyfikatorów rodziców (lista pusta dla korzenia)\n" )
  fmt.Fprintf( os.Stdout, "  children ([String]): tablica identyfikatorów dzieci (lista pusta dla liści)\n" )
  fmt.Fprintf( os.Stdout, "  wikidata_id (String): identyfikator kategorii w projekcie WikiData\n" )
  fmt.Fprintf( os.Stdout, "  is_meta (bool): true jeśli to jest metakategoria. W wynikowym pliku są tylko te metakategorie, które mają jakieś artykuły (namespace 0)\n" )
  fmt.Fprintf( os.Stdout, "  min_level: minimalny poziom głębokości kategorii w grafie\n" )
  fmt.Fprintf( os.Stdout, "  max_level: maksymalny poziom głębokości kategorii w grafie\n" )
  fmt.Fprintf( os.Stdout, "  articles_count: liczba artykułów w kategorii (ale nie w potomkach)\n" )
  fmt.Fprintf( os.Stdout, "  subtree_articles_count: (gdy uruchomiono z opcją 'full') liczba artykułów w kategorii i wszystkich jej potomkach\n" )
  fmt.Fprintf( os.Stdout, "  subtree_cats_count: (gdy uruchomiono z opcją 'full') liczba wszystkich kategorii potomnych + 1\n" )
  fmt.Fprintf( os.Stdout, "  langlinks: (tylko dla polecenia 'synthesis') mapa lang_code: tytuł kategorii. Uwaga: tytuł kategorii zawiera prefiks a spacje nie są zamieniane na znaki podkreślenia, np. 'Kategoria:Misio pysio'\n" )
}

func process_category_index_file( p map[string]string ) {
  page_id := utils.Atoi64(p["page_id"])
  page_title := p["page_title"]
  cat := Category {
    Id: page_id,
    Title: page_title,
    Parents: make([]int64,0),
    Children: make([]int64,0),
    IsMeta: false,
    localArticles: make(map[int64]bool),
    parents: make(map[int64]*Category),
    children: make(map[int64]*Category) }
  CatsByPageId[page_id] = &cat
}

func process_page_props_categories_file( p map[string]string ) {
  pp_propname := p["pp_propname"]
  if (pp_propname == "wikibase_item") || (pp_propname == "hiddencat") {
    pp_page := utils.Atoi64(p["pp_page"])
    cat := CatsByPageId[pp_page]
    if cat != nil {
      pp_value := p["pp_value"]
      if pp_propname == "wikibase_item" {
        cat.WikiDataId = pp_value
      } else {
        cat.isHidden = true
      }
    }
  }
}

func process_article2category_file( p map[string]string ) {
  articleId := utils.Atoi64(p["cl_from"])
  cl_to := utils.Atoi64(p["cl_to"])
  cat := CatsByPageId[cl_to]
  if cat != nil {
    cat.ArticlesCount += 1 // tu ważne tylko że > 0, do obcinania liści
    cat.localArticles[articleId] = true
  }
}

func process_category2category_file( p map[string]string ) {
  cl_from := utils.Atoi64(p["cl_from"])
  cl_to := utils.Atoi64(p["cl_to"])
  child := CatsByPageId[cl_from]
  parent := CatsByPageId[cl_to]
  if (child.Id != parent.Id) && (parent.parents[child.Id] == nil) && (child.children[parent.Id] == nil) {
    parent.children[child.Id] = child
    child.parents[parent.Id] = parent
  }
}

func DumpTraverse( cat *Category, enc *json.Encoder ) {
  enc.Encode( cat )
  for _, child := range cat.children {
    DumpTraverse( child, enc )
  }
}

func Accumulate( cat *Category, acc []*Category, was map[int64]bool ) []*Category {
  acc = append(acc, cat)
  was[cat.Id] = true
  for _, child := range cat.children {
    if !was[child.Id] {
      acc = Accumulate( child, acc, was )
    }
  }
  return acc
}

func dump(w io.WriteCloser) {
  enc := json.NewEncoder( w )
  for _, cat := range CatsByPageId {
    enc.Encode(cat)
  }
}

func countSubTree( node *Category, marks map[int64]bool ) int {
  sum := 0
  if marks == nil {
    marks = make(map[int64]bool)
  }
  if !marks[node.Id] {
    marks[node.Id] = true
    sum = 1
    for _, child := range node.children {
      sum += countSubTree( child, marks )
    }
  }
  return sum
}

var RemovedNodes int = 0
func removeNode( node *Category ) {
  // fmt.Fprintf( os.Stderr, "Remove node: '%s'\n", node.Title )
  for _, parent := range( node.parents ) {
    delete( parent.children, node.Id )
  }
  node.parents = nil
  for _, child := range( node.children ) {
    delete( child.parents, node.Id )
  }
  node.children = nil
  delete( CatsByPageId, node.Id )
  RemovedNodes += 1
}

var RemovedEdges int = 0
func removeEdge( parentNode *Category, childNode *Category ) {
  delete( parentNode.children, childNode.Id )
  delete( childNode.parents, parentNode.Id )
  RemovedEdges += 1
}

// Moze byc tylko jeden wierzcholek bez rodzicow
func oneComponent() {
  startTs := time.Now()
  fmt.Fprintf( os.Stderr, "Root check and remove unavailable nodes...\n" )
  if RootNode == nil {
    root_nodes := make(map[int64]int)
    for _, cat := range CatsByPageId {
      if len(cat.parents) == 0 {
        root_nodes[cat.Id] = countSubTree( cat, nil )
      }
    }
    root_node_ids := make([]int64,0,len(root_nodes))
    for cid, _ := range root_nodes {
      root_node_ids = append( root_node_ids, cid )
    }
    sort.Slice( root_node_ids, func(i, j int) bool {
      return !(root_nodes[root_node_ids[i]] < root_nodes[root_node_ids[j]])
    })
    if RootNode == nil {
      RootNode = CatsByPageId[root_node_ids[0]]
    }
    counter := 0
    for _, cid := range( root_node_ids ) {
      if counter < 10 {
        cat := CatsByPageId[cid]
        fmt.Fprintf( os.Stderr, "  Potential root: '%s' (%d), Size: %d\n", cat.Title, cat.Id, root_nodes[cid] )
      } else {
	fmt.Fprintf( os.Stderr, "  ...\n" )
	break
      }
      counter += 1
    }
    fmt.Fprintf( os.Stderr, "ROOT NODE: %s (%d)\n", RootNode.Title, RootNode.Id )
  }
  marks := make(map[int64]bool)
  countSubTree( RootNode, marks )
  counter := 0
  for cid, cat := range CatsByPageId {
    if !marks[cid] {
      removeNode( cat )
      counter += 1
    }
  }
  fmt.Fprintf( os.Stderr, "Done. Removed %d nodes [%.2f s]\n", counter, float64(time.Now().Sub(startTs))/1000000000.0 )
}

var CycleCounter int = 0
func findCycle( node *Category, edgesToRemove map[int64]map[int64]*Category, marks map[int64]bool ) int {
  if (node.isHidden) || ((MetaRootNode != nil) && (node == MetaRootNode)) {
    return 0
  }
  counter := 0
  nodeId := node.Id
  if marks == nil {
    marks = make(map[int64]bool)
  }
  if edgesToRemove == nil {
    edgesToRemove = make(map[int64]map[int64]*Category)
  }
  AccessibleNodes[nodeId] = node
  marks[nodeId] = true
  for childId, child := range node.children {
    if marks[childId] {
      CycleCounter += 1
      // fmt.Fprintf( os.Stderr, "%d. CYCLE DETECTED %s => %s\n", CycleCounter, node.Title, child.Title )
      if edgesToRemove[nodeId] == nil {
        edgesToRemove[nodeId] = make(map[int64]*Category)
      }
      edgesToRemove[nodeId][childId] = child
      counter += 1
    } else if AccessibleNodes[childId] == nil {
      counter += findCycle( child, edgesToRemove, marks )
    }
  }
  marks[nodeId] = false
  return counter
}

func globalFindCycle() {
  startTs := time.Now()
  fmt.Fprintf( os.Stderr, "Find cycles...\n" )
  edgesToRemove := make(map[int64]map[int64]*Category)
  totalCounter := 0
  for {
    cycleCounter := findCycle( RootNode, edgesToRemove, nil )
    fmt.Fprintf( os.Stderr, "  found %d\n", cycleCounter )
    totalCounter += cycleCounter
    if cycleCounter == 0 {
      break
    }
  }
  for parentId, children := range edgesToRemove {
    for _, child := range children {
      removeEdge( CatsByPageId[parentId], child )
    }
  }
  fmt.Fprintf( os.Stderr, "Done. Cycles detected: %d [%.2f s]\n", totalCounter, float64(time.Now().Sub(startTs))/1000000000.0 )
}

func removeUnusedLeafs() int {
  globalCounter := 0
  for {
    toRemove := make([]*Category, 0)
    for _, cat := range CatsByPageId {
      if (cat.ArticlesCount == 0) && (len(cat.children) == 0) {
        toRemove = append(toRemove, cat)
      }
    }
    // fmt.Fprintf( os.Stderr, "REMOVE LEAFS: %d\n", len(toRemove) )
    if len(toRemove) == 0 {
      break
    } else {
      globalCounter += len(toRemove)
      for _, cat := range toRemove {
        removeNode( cat )
      }
    }
  }
  return globalCounter
}

var AccArticles map[int64]bool
func subtreeArticles( node *Category ) {
  Marks[node.Id] = true
  for nodeId, _ := range node.localArticles {
    /*if len(AccArticles) >= 1000000 {
      return
    }*/
    AccArticles[nodeId] = true
  }
  for _, child := range node.children {
    if !Marks[child.Id] {
      subtreeArticles( child )
    }
  }
}

type SubtreeFun func( node *Category, accArticles map[int64]bool )

var GlobalSubtreeCounter int = 0
func GlobalSubtreeArticles( node *Category, graph map[int64]*Category, subtreeFun SubtreeFun ) {
  Marks = make(map[int64]bool)
  AccArticles = make(map[int64]bool)
  fmt.Fprintf( os.Stderr, "%d/%d. %s %s ", GlobalSubtreeCounter, len(graph), node.Id, node.Title )
  GlobalSubtreeCounter += 1
  if GlobalSubtreeCounter % 100000 == 0 {
    runtime.GC()
  }
  subtreeArticles( node )
  node.SubtreeArticlesCount = len(AccArticles)
  node.SubtreeCatsCount = len(Marks)
  fmt.Fprintf( os.Stderr, "[%d][%d]\n", node.SubtreeArticlesCount, node.SubtreeCatsCount )
  if node.SubtreeArticlesCount == 0 {
    fmt.Fprintf( os.Stderr, "NODE.SUBTREECOUNT == 0!!! %d/%d. %s [%d]\n", GlobalSubtreeCounter, len(CatsByPageId), node.Title, node.SubtreeArticlesCount )
    os.Exit(1)
  }
  if subtreeFun != nil {
    subtreeFun( node, AccArticles )
  }
  for _, child := range node.children {
    if child.SubtreeCatsCount == 0 {
      GlobalSubtreeArticles( child, graph, subtreeFun )
    }
  }
}

func WorkerSubtreeArticles( node *Category, worker *Worker ) {
  worker.Marks[node.Id] = true
  for nodeId, _ := range node.localArticles {
    worker.AccArticles[nodeId] = true
  }
  for _, child := range node.children {
    if !worker.Marks[child.Id] {
      WorkerSubtreeArticles( child, worker )
    }
  }
}

func GlobalWorkerSubtreeArticles( worker *Worker, ch chan *Category ) {
  counter := 0
  for node := range ch {
    worker.Marks = make(map[int64]bool)
    worker.AccArticles = make(map[int64]bool)
    WorkerSubtreeArticles( node, worker )
    node.SubtreeArticlesCount = len(worker.AccArticles)
    node.SubtreeCatsCount = len(worker.Marks)
    //for key, _ := range worker.Marks { delete(worker.Marks, key) }
    //for key, _ := range worker.AccArticles { delete(worker.AccArticles, key) }
    counter += 1
  }
  worker.Marks = nil
  worker.AccArticles = nil
  fmt.Fprintf( os.Stderr, "Worker %d; Counter %d\n", worker.Id, counter )
}

func test() {
  fmt.Fprintf( os.Stderr, "#### TEST #####\n" )
  for _, cat := range MetaNodes {
    for _, child := range cat.children {
      if !child.IsMeta {
        fmt.Fprintf( os.Stderr, "Meta-nie-meta: '%s' (%s) -> '%s' (%s)\n", cat.Title, cat.Id, child.Title, child.Id )
      }
    }
  }
}

// Wykorzystywane także przez pakiet 'tiger.com.pl/wikidumptools/synthesis'
func ComputeGraphProps( rootNode *Category, graph map[int64]*Category, FullFlag bool ) {
  startTs := time.Now()
  fmt.Fprintf( os.Stderr, "Counting ArticlesCount for each category...\n" )
  for _, cat := range graph {
    cat.ArticlesCount = len( cat.localArticles )
    AllArticlesCatCount += len( cat.localArticles )
  }
  fmt.Fprintf( os.Stderr, "Done [%.2f s]\n", float64(time.Now().Sub(startTs))/1000000000.0 )
  if FullFlag {
    startTs := time.Now()
    fmt.Fprintf( os.Stderr, "Counting SubtreeArticlesCount and SubtreeCatsCount for each category...\n" )
    // fmt.Fprintf( os.Stderr, "[1] %d %d\n", AllArticlesCatCount, len(CatsByPageId) )
    WorkersCount := runtime.GOMAXPROCS(-1)
    fmt.Fprintf( os.Stderr, "Workers count: %d\n", WorkersCount )
    var wg sync.WaitGroup
    wg.Add(WorkersCount)
    ch := make(chan *Category, len(graph))
    for workerNo := 0; workerNo < WorkersCount; workerNo++ {
      go func(workerNo int) {
        defer wg.Done()
	GlobalWorkerSubtreeArticles( &Worker{ Id: workerNo }, ch )
      }(workerNo)
    }
    // delta := len(graph)/100
    // counter := 0
    for _, node := range graph {
      ch <- node
      /*
      counter += 1
      if counter % delta == 0 {
	s := "."
	if counter % (delta * 10) == 0 {
	  s = "*"
	}
	fmt.Fprintf( os.Stderr, s )
      }
      */
    }
    // fmt.Fprintf( os.Stderr, "\n" )
    close(ch)
    wg.Wait()
    fmt.Fprintf( os.Stderr, "Done [%.2f s]\n", float64(time.Now().Sub(startTs))/1000000000.0 )
    // GlobalSubtreeArticles( rootNode, CatsByPageId, nil )
  }
  startTs = time.Now()
  fmt.Fprintf( os.Stderr, "Converting cat.children to cat.Children and the same with parents...\n" )
  for _, cat := range graph {
    cat.Children = make([]int64, len(cat.children))
    i := 0
    for cid, _ := range cat.children {
      cat.Children[i] = cid
      i += 1
    }
    cat.Parents = make([]int64, len(cat.parents))
    i = 0
    for pid, _ := range cat.parents {
      cat.Parents[i] = pid
      i += 1
    }
  }
  fmt.Fprintf( os.Stderr, "Done [%.2f s]\n", float64(time.Now().Sub(startTs))/1000000000.0 )
  Marks = make(map[int64]bool)
  fmt.Fprintf( os.Stderr, "  Nodes in graph: %d\n", countSubTree( rootNode, nil ) )
}

// Wykorzystywane także przez pakiet 'tiger.com.pl/wikidumptools/synthesis'
func Stats( graph map[int64]*Category, FullFlag bool ) {
  leafsCount := 0
  childrenSum := 0
  parentsSum := 0
  for _, cat := range graph {
    parentsSum += len(cat.parents)
    childrenSum += len(cat.children)
    if len(cat.children) == 0 {
      leafsCount += 1
    }
  }
  fmt.Fprintf( os.Stderr, "  Leafs: %d\n", leafsCount )
  fmt.Fprintf( os.Stderr, "  Avg children in internal nodes: %.2f\n", float32(childrenSum)/float32(len(graph)-leafsCount) )
  fmt.Fprintf( os.Stderr, "  Avg parents: %.2f\n", float32(parentsSum)/float32(len(graph)) )
  if FullFlag {
    fmt.Fprintf( os.Stderr, "  Avg articles per category: %.2f\n", float32(AllArticlesCatCount)/float32(len(graph)) )
  }
}

var OutputDir string
var FullFlag bool
var AllArticlesCatCount int = 0

func Main( args []string ) bool {
  if len(args) < 2 {
    return false
  }
  LangWiki = args[1]
  if LangWiki == "doc" {
    dumpDoc()
    return true
  }
  LangCode, OutputDir = utils.ParseLangWiki( LangWiki, false )
  if len(args) > 2 {
    FullFlag = args[2] == "full"
  }

  startTs := time.Now()
  fmt.Fprintf( os.Stderr, "Loading data files...\n" )
  utils.ProcessLocalFile( path.Join( OutputDir, "category_index.tsv" ), process_category_index_file, "\t", nil )
  utils.ProcessLocalFile( path.Join( OutputDir, "page_props_categories.tsv" ), process_page_props_categories_file, "\t", nil )
  utils.ProcessLocalFile( path.Join( OutputDir, "article2category.tsv" ), process_article2category_file, "\t", nil )
  utils.ProcessLocalFile( path.Join( OutputDir, "category2category.tsv" ), process_category2category_file, "\t", nil )
  fmt.Fprintf( os.Stderr, "Done [%.2f s]\n", float64(time.Now().Sub(startTs))/1000000000.0 )

  // Wyrzucamy kategorie ukryte
  startTs = time.Now()
  fmt.Fprintf( os.Stderr, "Removing hidden categories...\n" )
  toRemove := make([]*Category,0)
  for _, cat := range CatsByPageId {
    if cat.isHidden {
      toRemove = append(toRemove, cat)
    }
  }
  for _, cat := range toRemove {
    removeNode( cat )
  }
  fmt.Fprintf( os.Stderr, "Done: %d removed [%.2f s]\n", len(toRemove), float64(time.Now().Sub(startTs))/1000000000.0 )

  // Być może korzeń grafu kategorii mamy podany na tacy
  startTs = time.Now()
  fmt.Fprintf( os.Stderr, "Looking for root node...\n" )
  if strings.HasSuffix( LangWiki, "wiki" ) { // a nie np. 'wikiquote'
    masterPageId, ok := WikipediaMasterCats[LangCode]
    if ok {
      RootNode = CatsByPageId[masterPageId]
      if RootNode != nil {
        // korzeń nie może mieć rodziców
        toRemove := make([]*Category,0)
        for _, parent := range RootNode.parents {
          toRemove = append(toRemove, parent)
        }
        for _, parent := range toRemove {
          removeEdge( parent, RootNode )
        }
        fmt.Fprintf( os.Stderr, "  ROOT NODE: %s (%d)\n", RootNode.Title, RootNode.Id )
      }
    }
  }
  fmt.Fprintf( os.Stderr, "Done [%.2f s]\n", float64(time.Now().Sub(startTs))/1000000000.0 )

  fmt.Fprintf( os.Stderr, "FIRST ROUND...\n" )
  oneComponent()
  globalFindCycle()
  fmt.Fprintf( os.Stderr, "SECOND ROUND...\n" )
  oneComponent()
  globalFindCycle()

  // Usuwamy wierzchołki niedostępne z korzenia
  nodesToRemove := make([]*Category,0)
  for cid, cat := range CatsByPageId {
    if AccessibleNodes[cid] == nil {
      nodesToRemove = append( nodesToRemove, cat )
    }
  }
  for _, cat := range nodesToRemove {
    removeNode( cat )
  }
  fmt.Fprintf( os.Stderr, "Unnaccessible nodes: %d\n", len(nodesToRemove) )

  // Rekurencyjnie usuwamy kategorie-liście bez artykułów
  startTs = time.Now()
  fmt.Fprintf( os.Stderr, "Remove category leafs without articles...\n" )
  unusedLeafsCounter := removeUnusedLeafs()
  fmt.Fprintf( os.Stderr, "Done, removed %d nodes [%.2f s]\n", unusedLeafsCounter, float64(time.Now().Sub(startTs))/1000000000.0 )

  startTs = time.Now()
  fmt.Fprintf( os.Stderr, "Find metacategories...\n" )
  if strings.HasSuffix( LangWiki, "wiki" ) { // a nie np. 'wikiquote'
    metaRootId, ok := WikipediaRootMetaCats[LangCode]
    if ok {
      mrn := CatsByPageId[metaRootId]
      if mrn != nil {
        AccessibleNodes = make(map[int64]*Category)
        findCycle( mrn, nil, nil )
        MetaNodes = AccessibleNodes // wierzchołki dostępne z 'Kategoria:Metakategorie'
	fmt.Fprintf( os.Stderr, "Meta nodes count: %d\n", len(MetaNodes) )
        AccessibleNodes = make(map[int64]*Category)
        MetaRootNode = mrn // wazne zeby przypisanie bylo w tym miejscu, a nie wyzej!
        findCycle( RootNode, nil, nil )
        // AccessibleNodes: wierzchołki dostępne z 'Kategoria:Kategorie', ale nie z 'Kategoria:Metakategorie'
        toRemove := make([]int64,0)
        for nodeId, cat := range MetaNodes {
          if AccessibleNodes[nodeId] == cat {
            toRemove = append(toRemove, nodeId )
          } else {
            cat.IsMeta = true
            // fmt.Fprintf( os.Stderr, "META: '%s'\n", cat.Title )
          }
        }
        for _, nodeId := range toRemove {
          delete( MetaNodes, nodeId )
        }
      }
    }
  }
  fmt.Fprintf( os.Stderr, "Done. Found %d metacategories [%.2f s]\n", len(MetaNodes), float64(time.Now().Sub(startTs))/1000000000.0 )

  ComputeGraphProps( RootNode, CatsByPageId, FullFlag )

  file, err := os.Create( path.Join( OutputDir, "categories.json.gz" ) )
  if err != nil {
    panic( err )
  }
  defer file.Close()
  gz := gzip.NewWriter( file )
  dump( gz )
  gz.Flush()
  gz.Close()

  fmt.Fprintf( os.Stderr, "Stats:\n" )
  fmt.Fprintf( os.Stderr, "  Removed nodes: %d\n", RemovedNodes )
  fmt.Fprintf( os.Stderr, "  Removed edges: %d\n", RemovedEdges )
  fmt.Fprintf( os.Stderr, "  Cycles: %d\n", CycleCounter )
  fmt.Fprintf( os.Stderr, "  Meta nodes (have articles!): %d\n", len(MetaNodes) )

  Stats( CatsByPageId, FullFlag )

  // test()
  return true
}

// Identyfikatory kategorii głównych dla różnych wersji językowych Wikipedii (dla 'pl' jest to 'Kategoria:Kategorie')
var WikipediaMasterCats = map[string]int64 {
  "pl":1382275,"ady":675,"af":6362,"als":1930,"am":8990,"an":63313,"ar":478217,"azb":9498,"be-x-old":175016,
  "bh":33109,"bn":129694,"ba":4179,"arz":145383,"as":9448,"ast":67007,"az":466069,"br":132107,"bs":71899,
  "bxr":6677,"ce":5768,"chr":3888,"chy":3009,"ckb":133859,"cu":6356,"cv":16991,"da":472786,"en":14104879,
  "eo":354065,"es":313280,"eu":675535,"fa":266225,"fr":3602955,"he":160051,"hi":78272,"hy":70432,"id":465109,
  "ilo":12929,"inh":214,"jam":2592,"jv":43893,"ka":333859,"kaa":421,"ki":1830,"kk":73727,"ko":1726700,
  "koi":1862,"krc":1759,"ku":52310,"kv":2576,"ky":5232,"lo":8470,"map-bms":13282,"mhr":4436,"mk":51082,
  "ml":53813,"mr":89809,"ms":117653,"mwl":30,"my":75705,"ne":96139,"ps":10906,"pt":1973032,"ro":465593,
  "ru":5861034,"sah":2223,"sco":6293,"sd":43559,"sh":150396,"si":27309,"simple":137597,"sl":160214,"sq":84164,
  "sr":566540,"su":46738,"sv":6579495,"tg":49762,"th":971708,"tk":5744,"tn":1670,"tr":1249324,"tt":16537,
  "udm":2215,"uk":953711,"ur":52760,"uz":645096,"vi":1446224,"wa":28172,"yo":31274,"zh":6349209,"zh-yue":176535,
  "zu":7336,"csb":2638,"ay":2546,"bar":327,"be":107206,"bg":27113,"bm":3069,"bo":2939,"ca":13225,"cbk-zam":2069,
  "cdo":5142,"ceb":35161,"co":11692,"crh":661,"cs":4660,"de":235489,"din":139,"diq":11602,"dsb":2272,"dz":2778,
  "el":2609,"eml":34336,"et":47062,"ext":152,"fi":65378,"fo":4490,"frp":4607,"frr":999,"fur":9274,"fy":7762,
  "ga":2861,"gag":1413,"gan":1636,"gd":4530,"gl":37928,"gn":2629,"hak":765,"hr":613779,"hsb":389,"ht":2497,
  "hu":548046,"ia":6041,"ig":4179,"io":3660,"is":2089,"it":41474,"ja":185451,"kab":44,"kbd":1405,"kbp":11,
  "kg":4428,"kl":7085,"km":12831,"kn":81274,"ksh":1697,"kw":5687,"la":7321,"lad":4830,"lb":23579,"lez":1155,
  "li":1383,"lmo":26659,"ln":3007,"ltg":737,"mai":3331,"mdf":364,"min":4119,"mn":3605,"mrj":1001,"mt":1169,
  "myv":916,"mzn":20260,"nah":21359,"nap":24355,"nds":2725,"nds-nl":493,"nl":63579,"no":15527,"cy":12098,
  "arc":5300,"bat-smg":7070,"gv":8886,"ie":2291,"ik":2934,"lt":1771,"lv":6913,"mi":4013,"nn":1864,"ny":1905,
  "oc":48840,"pap":1388,"pcd":5732,"roa-tara":1713,"rw":1798,"szl":30,"ta":305330,"tl":41183,"vo":174399,
  "xmf":5536,"yi":41804,"zh-min-nan":720776,"ve":2656,"nso":837,"or":58458,"os":351,"pa":80898,"pdc":8701,
  "pfl":543,"pms":98865,"pnt":307,"qu":38496,"rm":5039,"rmy":2021,"rue":2145,"sa":18884,"sc":3083,"se":6326,
  "sg":2920,"sk":5674,"srn":20,"ss":2046,"stq":2826,"sw":6694,"te":63606,"tet":1591,"ts":2625,"ty":1700,
  "tyv":617,"ug":13328,"vec":1775,"vep":1402,"vls":1965,"wo":3923,"wuu":2749,"za":11390,"zea":31,
  "zh-classical":427,"lij":3650,"scn":34286}

/* Identyfikatory odpowiedników kategorii 'Kategoria:Metastrony Wikipedii' dla różnych wersji językowych Wikipedii
   (przestrzeń nazw nieprzeznaczonych dla artykułów). */
var WikipediaRootMetaCats = map[string]int64 {
  "pl":2545908,"af":10692,"als":1929,"am":1621,"an":394,"ar":267021,"arz":145390,"as":8247,"ast":44830,"av":1631,
  "ay":2545,"az":479208,"azb":25359,"ba":70632,"bar":7452,"be":83198,"be-x-old":3678,"bh":10309,"bi":3607,
  "bn":2588,"bo":2945,"bs":400142,"ca":317682,"cdo":4339,"ce":39040,"ckb":91289,"cs":190881,"csb":2488,"cu":6456,
  "cv":11109,"cy":67525,"de":2123792,"din":168,"el":18876,"en":2953378,"eo":416936,"es":5967891,"et":21055,
  "eu":21445,"ext":7888,"fa":49566,"fi":23879,"fr":382342,"frp":6849,"fur":4411,"gag":4203,"gan":1676,"gl":4148,
  "gv":12049,"hak":18865,"he":24589,"hi":98988,"hif":3467,"hr":613780,"hsb":22076,"hu":54798,"hy":79735,
  "id":2656103,"ilo":18132,"inh":56,"io":21109,"it":700153,"ja":3898160,"jv":157487,"ka":407349,"kbp":2215,
  "kk":20731,"ko":39546,"ku":59741,"ky":116068,"lfn":2083,"lmo":133510,"lt":12859,"lv":165039,"mai":31543,
  "mhr":4004,"mk":64717,"ml":55226,"mn":83798,"mr":197812,"ms":29044,"mwl":4750,"my":56046,"mzn":20252,
  "nap":19745,"nds-nl":15388,"nl":110048,"no":154569,"oc":32609,"pcd":3768,"pms":98866,"pt":2292351,"ro":50827,
  "ru":7938,"sa":18782,"sah":13216,"sco":66798,"sd":12148,"se":13253,"sh":4030373,"simple":176788,"sk":163748,
  "sl":343894,"sq":168446,"sr":76415,"sv":69281,"sw":8035,"szl":4403,"ta":49761,"tg":69084,"th":38725,"tl":41174,
  "tr":92185,"ts":2259,"tt":58429,"udm":2262,"uk":43095,"ur":88835,"uz":2445,"vep":2403,"vi":5945,"vls":1444,
  "wa":20875,"war":30,"wuu":2909,"yi":16182,"yo":15452,"zea":62,"zh":501839,"zh-classical":621,"zh-min-nan":584947,
  "zh-yue":4018}

