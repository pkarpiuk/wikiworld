package wikidumptools

import "fmt"
import "bufio"
import "os"
import "strings"
import "regexp"
import "io"
import "compress/gzip"
import "compress/bzip2"
import "net/http"
import "strconv"
import "path"
import _ "github.com/mattn/go-sqlite3"

var SupportedLanguages map[string]bool = map[string]bool { "en": true, "de": true, "fr": true, "ru": true, "it": true, "es": true, "pl": true, "pt": true, "ja": true }

type ProcessFun func( map[string]string )

func processPreamble( str string ) (tableName string, columnNames []string) {
  columnNames = make( []string, 0, 100 )
  re, _ := regexp.Compile( "(?s)CREATE TABLE `([^`]+)`.*?\\(\\s+(.*)$" )
  matches := re.FindStringSubmatch( str )
  if matches != nil {
    tableName = matches[1]
    lines := strings.Split( matches[2], "\n" )
    re, _ = regexp.Compile( "(?m)^\\s*`([^`]+)`" )
    for _, line := range lines {
      matches = re.FindStringSubmatch( line )
      if matches != nil {
        columnNames = append( columnNames, matches[1] )
      } else {
        break
      }
    }
    // fmt.Println( strings.Join( columnNames, "\t" ) )
  } else {
    fmt.Fprintln(os.Stderr, "Error: Cannot find 'CREATE TABLE ' instruction")
    os.Exit(1)
  }
  return
}

var PGSColumnNames []string

func ProcessGzippedStream( streamName string, r io.Reader, fn ProcessFun ) {
  fmt.Fprintf( os.Stderr, "Processing %s... ", streamName )
  gz, err := gzip.NewReader(r)
  if err != nil {
    panic( err )
  }
  defer gz.Close()

  var preamble strings.Builder
  var val strings.Builder
  buffer := make( []string, 0, 100 )
  scanner := bufio.NewScanner(gz)
  buf := make([]byte, 0, 10*1024*1024)
  scanner.Buffer(buf, 10*1024*1024)
  wasInsert := false
  columnCount := 0
  //var tableName string
  recordCounter := 0
  paramHash := make( map[string]int )
  params := make( map[string]string )
  for scanner.Scan() {
    line := scanner.Text()
    if strings.HasPrefix( line, "INSERT " ) {
      if !wasInsert {
        wasInsert = true
        _, PGSColumnNames = processPreamble( preamble.String() )
        for index, columnName := range PGSColumnNames {
          paramHash[columnName] = index
        }
        columnCount = len(PGSColumnNames)
      }
      inP, inS, inQ := false, false, false
      for _, r := range line {
        if r == '\t' {
          r = ' '
        }
        if !inP {
          if r == '(' {
            inP = true
          }
        } else {
          if !inS {
            if r == '\'' {
              inS = true
            } else {
              if (r == ',') || (r == ')') {
                buffer = append( buffer, val.String() )
                val.Reset()
                if r == ')' {
                  inP = false
                  if( len(buffer) != columnCount ) {
                    fmt.Fprintf( os.Stderr, "Error: Invalid column count: should be %d, but is %d\n%s\n", columnCount, len(buffer), strings.Join( buffer, "\t" ) )
                    os.Exit( 1 )
                  }
                  for pname, index := range paramHash {
                    value := buffer[index]
                    if value == "NULL" {
                      value = ""
                    }
                    params[pname] = value
                  }
                  fn( params )
                  recordCounter += 1
                  if recordCounter % 1000000 == 0 {
                    fmt.Fprintf( os.Stderr, "#" )
                  }
                  buffer = buffer[:0]
                }
              } else {
                val.WriteRune( r )
              }
            }
          } else {
            if !inQ {
              if r == '\'' {
                inS = false
              } else if r == '\\' {
                inQ = true
              } else {
                val.WriteRune( r )
              }
            } else {
              val.WriteRune( r )
              inQ = false
            }
          }
        }
      }
    } else {
      if !wasInsert {
        preamble.WriteString( line )
        preamble.WriteRune( '\n' )
      }
    }
  }
  if err = scanner.Err(); err != nil {
    panic( err )
  }

  fmt.Fprintf( os.Stderr, " %d\n", recordCounter )
}

func ProcessTsvFile( streamName string, r io.Reader, fn ProcessFun, separator string, paramHash map[string]int ) {
  fmt.Fprintf( os.Stderr, "Processing %s... ", streamName )
  lineCounter := 0
  paramNames := make( []string, 0, 100 )
  if paramHash == nil {
    paramHash = make( map[string]int )
  }
  params := make( map[string]string )
  scanner := bufio.NewScanner(r)
  buf := make([]byte, 0, 10*1024*1024)
  scanner.Buffer(buf, 10*1024*1024)
  for scanner.Scan() {
    line := scanner.Text()
    // fmt.Fprintf( os.Stderr, "%s\n", line )
    if len(paramHash) == 0 {
      paramNames = strings.Split( line, separator )
      for index, paramName := range paramNames {
        paramHash[paramName] = index
      }
    } else {
      values := strings.Split( line, separator )
      if len(values) != len(paramHash) {
        // fmt.Fprintf( os.Stderr, "Invalid line: '%s' (%d columns, should be %d)\n", line, len(values), len(paramHash) )
        fmt.Fprintf( os.Stderr, "!" )
        continue
      }
      for pname, index := range paramHash {
        value := values[index]
        if value == "NULL" {
          value = ""
        }
        params[pname] = value
      }
      fn( params )
      lineCounter += 1
      if lineCounter % 1000000 == 0 {
        fmt.Fprintf( os.Stderr, "#" )
      }
    }
  }
  fmt.Fprintf( os.Stderr, " %d\n", lineCounter )
}

func ProcessLocalFile( fpath string, fn ProcessFun, separator string, paramHash map[string]int ) {
  var r io.Reader
  // sprawdzamy czy jest wersja spakowana
  gfpath := fpath + ".gz"
  if _, err := os.Stat(gfpath); err == nil {
    fpath = gfpath
  }
  bfpath := fpath + ".bz2"
  if _, err := os.Stat( bfpath ); err == nil {
    fpath = bfpath
  }
  file, err := os.Open( fpath )
  if err != nil {
    panic( err )
  }
  if fpath == bfpath {
    br := bufio.NewReader(file)
    bz2 := bzip2.NewReader(br)
    defer file.Close()
    r = bz2
  } else if fpath == gfpath {
    gz, err := gzip.NewReader(file)
    if err != nil {
      panic( err )
    }
    defer file.Close()
    defer gz.Close()
    r = gz
  } else {
    r = file
    defer file.Close()
  }
  ProcessTsvFile( fpath, r, fn, separator, paramHash )
}

func HTTPStream( url string ) io.ReadCloser {
  resp, err := http.Get(url)
  if err != nil {
    panic(err)
  }
  return resp.Body;
}

func FileStream( fpath string ) io.ReadCloser {
  var r io.ReadCloser
  // sprawdzamy czy jest wersja spakowana
  gfpath := fpath + ".gz"
  if _, err := os.Stat(gfpath); err == nil {
    fpath = gfpath
  }
  file, err := os.Open(fpath)
  if err != nil {
    panic(err)
  }
  if fpath == gfpath {
    gz, err := gzip.NewReader(file)
    if err != nil {
      panic( err )
    }
    r = gz
  } else {
    r = file
  }
  return r
}

func CreateFile( fpath string ) *os.File {
  file, err := os.Create( fpath )
  if err != nil {
    panic( err )
  }
  os.Chmod(fpath, 0755)
  return file
}

func CreateGzFile( fpath string ) (*os.File, *gzip.Writer) {
  file := CreateFile( fpath )
  gz := gzip.NewWriter( file )
  return file, gz
}

func Atoi( str string ) int {
  result, err := strconv.Atoi( str )
  if err != nil {
    panic(err)
  }
  return result
}

var DataDir string = "/db"
var PublicDataDir string = "/db/public"
var DumpDateStr = "current"

func ParseLangWiki( langWiki string, createLangDirIfNotExists bool ) ( langCode string, langDataDir string ) {
  index := strings.Index(langWiki, "wiki" )
  langCode = langWiki[0:index]
  subdirName := langWiki[index:]
  if subdirName == "wiki" {
    subdirName = "wikipedia"
  }
  langDataDir = path.Join( DataDir, "cache", "dumps", subdirName, langCode, DumpDateStr )
  if _, err := os.Stat(langDataDir); os.IsNotExist(err) {
    if createLangDirIfNotExists && DumpDateStr != "current" {
      os.MkdirAll( langDataDir, 0777 )
    } else {
      panic( "Wiki data dir '" + langDataDir + "' does not exist. DATA_DIR environment variable is unset or invalid?" )
    }
  }
  return
}

func ParsePublicLangWiki( langWiki string, createLangDirIfNotExists bool ) ( langCode string, langDataDir string ) {
  index := strings.Index(langWiki, "wiki" )
  langCode = langWiki[0:index]
  subdirName := langWiki[index:]
  if subdirName == "wiki" {
    subdirName = "wikipedia"
  }
  langDataDir = path.Join( PublicDataDir, subdirName, langCode, DumpDateStr )
  if _, err := os.Stat(langDataDir); os.IsNotExist(err) {
    if createLangDirIfNotExists && DumpDateStr != "current" {
      os.MkdirAll( langDataDir, 0777 )
    } else {
      panic( "Wiki data dir '" + langDataDir + "' does not exist. DATA_DIR environment variable is unset or invalid?" )
    }
  }
  return
}

func Atoi64( s string ) int64 {
  i, err := strconv.ParseInt(s, 10, 64)
  if err != nil {
    panic(err)
  }
  return i
}

func mkDir( dir_path string ) {
  if _, err := os.Stat(dir_path); os.IsNotExist(err) {
    err = os.MkdirAll( dir_path, 0777 )
    if err != nil {
      panic( err )
    }
  }
}

func init() {
  if os.Getenv( "DATA_DIR" ) != "" {
    DataDir = os.Getenv( "DATA_DIR" )
    PublicDataDir = path.Join( DataDir, "public" )
  }
  if _, err := os.Stat(DataDir); os.IsNotExist(err) {
    panic( "Output dir '" + DataDir + "' does not exist. Set DATA_DIR environment variable properly" )
  }
  mkDir( path.Join( DataDir, "cache", "pageview", "months" ) )
  mkDir( path.Join( DataDir, "cache", "pageview", "days" ) )
  mkDir( path.Join( DataDir, "cache", "pageview", "hours" ) )
  mkDir( path.Join( DataDir, "cache", "dumps", "wikipedia" ) )
  mkDir( path.Join( DataDir, "cache", "dumps", "wikiquote" ) )
  mkDir( path.Join( DataDir, "cache", "clickstream" ) )
  mkDir( path.Join( DataDir, "public" ) )
  mkDir( path.Join( DataDir, "logs" ) )
  mkDir( path.Join( DataDir, "db" ) )
}

