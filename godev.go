// helps in developing the application by having
// source code change detection, recompiliation,
// and rerunning.
package main

import (
  // "errors"
  //goflag "flag"
  "github.com/howeyc/fsnotify"
  "github.com/jessevdk/go-flags"
  "github.com/kballard/go-shellquote"
  gobuild "go/build"
  "go/parser"
  "go/token"
  "log"
  "os"
  "os/exec"
  "path/filepath"
  "regexp"
  "strconv"
  "strings"
  "sync"
  "time"
)

type Options struct {
  Args    string   `short:"a" long:"args" description:"Arguments to pass to the program"`
  Include []string `short:"i" default:"" long:"include" description:"Regexp of dirs to include for watching. Can be specified multiple times. Default: .*"`
  Exclude []string `short:"e" default:"" long:"exclude" description:"Regexp of dirs to exclude from watching. Can be specified multiple times. Default: ^\.*$"`
  Files   []string `short:"f" default:"" long:"files" description:"Regexp of files that, if changed, will cause a build/run. Can be specified multiple times. Default: ^(.*\.go|.*\.yaml|.*\.conf)$"`

  include []*regexp.Regexp
  exclude []*regexp.Regexp
  files   []*regexp.Regexp
}

var (
  wpaths = make(map[string]string)
  // curPkg  = ""
  dir     = ""
  program = ""
  opts    = &Options{}
  // flagSet = goflag.NewFlagSet(os.Args[0], goflag.ExitOnError)
  // args    = flagSet.String("args", "", "Arguments to pass to the program")
  // include = flagSet.String("include", "*", "Pattern of dirs to include")
  // exclude = flagSet.String("exclude", "", "Pattern of dirs to exclude")
)

func parseArgs() []string {
  s, err := shellquote.Split(opts.Args)
  if err != nil {
    panic(err)
  }

  return s
}

func build() error {
  log.Printf("Rebuilding %s...\n", program)
  cmd := exec.Command("go", "build")
  cmd.Stdout = os.Stdout
  cmd.Stderr = os.Stderr
  err := cmd.Run()
  if err != nil {
    return err
  }

  return nil
}

func run() (*exec.Cmd, error) {
  log.Printf("Running %s...\n", program)
  cmd := exec.Command(filepath.Join(dir, program), parseArgs()...)
  cmd.Stdout = os.Stdout
  cmd.Stderr = os.Stderr

  err := cmd.Start()
  if err != nil {
    return nil, err
  }

  return cmd, nil
}

func buildAndRun() (*exec.Cmd, error) {
  err := build()
  if err != nil {
    return nil, err
  } else {
    cmd, err := run()
    if err != nil {
      return nil, err
    }
    return cmd, nil
  }

  panic("unreachable")
  return nil, nil
}

func shouldRerun(name string) (ret bool) {
  ret = false

  // um... should be configurable?
  ret = !strings.HasPrefix(filepath.Base(name), ".")
  if !ret {
    return
  }

  for _, r := range opts.files {
    if r.MatchString(name) {
      ret = true
      return
    }
  }

  return false
}

func watcher() {
  log.Println("Running watcher")

  var wg sync.WaitGroup
  watcher, err := fsnotify.NewWatcher()
  if err != nil {
    log.Fatal(err)
  }

  wg.Add(1)
  go func() {
    defer wg.Done()
    cmd, err := buildAndRun()
    if err != nil {
      log.Println(err)
    }

    for {
      select {
      case ev := <-watcher.Event:
        // basic throttling. we only do something when
        // we receive no file system events after
        // a certain time
      LOOP:
        for {
          select {
          case <-watcher.Event:
            continue LOOP
          case <-time.After(300 * time.Millisecond):
            break LOOP
          }
        }

        if ev.IsModify() || ev.IsCreate() {
          if shouldRerun(ev.Name) {
            if cmd != nil && cmd.Process != nil {
              log.Printf("Killing %s...\n", program)
              cmd.Process.Kill()
              cmd.Wait()
            }

            cmd, err = buildAndRun()
            if err != nil {
              log.Println(err)
            }
          }
        }
      case <-watcher.Error:
        //log.Println("error:", err)
      }
    }
  }()

  for _, value := range wpaths {
    err = watcher.Watch(value)
    if err != nil {
      log.Fatal(err)
    }
  }

  wg.Wait()
}

// func usage() {
//   fmt.Printf("Usage of %s:\n", os.Args[0])
//   fmt.Printf("  %s [args] [program]\n\n", os.Args[0])
//   fmt.Printf("Args:\n")
//   usagePrintDefaults()
//   fmt.Println("")
// }

// func usagePrintDefaults() {
//   flagSet.VisitAll(func(flag *goflag.Flag) {
//     format := "  -%s=%s\t%s\n"
//     // if _, ok := flag.Value.(*stringValue); ok {
//     //   // put quotes on the value
//     //   format = "  -%s=%q: %s\n"
//     // }
//     fmt.Printf(format, flag.Name, flag.DefValue, flag.Usage)
//   })
// }

// find where on the fs a package is
func which(pkg string) string {
  for _, top := range strings.Split(os.Getenv("GOPATH"), ":") {
    dir := top + "/src/" + pkg
    _, err := os.Stat(dir)
    if err == nil {
      return dir
    }
    p := err.(*os.PathError)
    if !os.IsNotExist(p.Err) {
      log.Print(err)
    }
  }
  return ""
}

func shouldWatch(path string) bool {
  for _, r := range opts.exclude {
    if r.MatchString(path) {
      log.Println("exclude:", path)
      return false
    }
  }

  for _, r := range opts.include {
    if r.MatchString(path) {
      return true
    }
  }

  return false
}

func getWatchDirs(path string, info os.FileInfo, err error) error {
  if err != nil {
    log.Print(err)
  }

  if info.IsDir() {
    // add all dirs we encounter
    if shouldWatch(path) {
      wpaths[path] = path
    }
  } else if !info.IsDir() && strings.HasSuffix(path, ".go") {
    // parse the go file and add all imports
    err = getWatchDirsFromFile(path)
    if err != nil {
      log.Print(err)
    }
  }
  return nil
}

// finds all the watch directories from the imports of the file
func getWatchDirsFromFile(path string) error {
  fset := token.NewFileSet()
  f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
  if err != nil {
    return err
  }

  for _, s := range f.Imports {
    path, err := strconv.Unquote(s.Path.Value)
    if err != nil {
      return err // can't happen
    }

    wpath := which(path)

    if wpath != "" {
      if shouldWatch(wpath) {
        wpaths[wpath] = wpath
      }
    }
  }

  return nil
}

// func getCurrentPkg() (string, error) {
//   gopath := os.Getenv("GOPATH")
//   if gopath == "" {
//     return "", errors.New("missing GOPATH")
//   }

//   dot, err := os.Getwd()
//   if err != nil {
//     return "", err
//   }

//   items := strings.Split(gopath, ":")
//   for _, top := range items {
//     top = top + "/src/"
//     if strings.HasPrefix(dot, top) {
//       return dot[len(top):], nil
//     }
//   }

//   return "", errors.New("cwd not found in GOPATH")
// }

func main() {
  var err error

  dir, _ = os.Getwd()

  opts = &Options{}
  args, err := flags.Parse(opts)
  if err != nil {
    os.Exit(1)
  }

  if len(opts.Include) == 0 {
    opts.Include = []string{`.*`}
  }
  if len(opts.Exclude) == 0 {
    opts.Exclude = []string{`^\.*$`}
  }
  if len(opts.Files) == 0 {
    opts.Files = []string{`^(.*\.go|.*\.yaml|.*\.conf)$`}
  }

  // flagSet.Usage = usage
  // flagSet.Parse(os.Args[1:])
  // program = flagSet.Arg(0)

  if len(args) > 0 {
    program = args[0]
  }

  // attempt to find program name
  if program == "" {
    _, spath := filepath.Split(dir)

    exeSuffix := ""
    if gobuild.Default.GOOS == "windows" {
      exeSuffix = ".exe"
    }

    program = spath + exeSuffix
  }

  // curPkg, err = getCurrentPkg()
  // if err != nil {
  //   fmt.Println(err)
  // }

  // build regexps
  for _, r := range opts.Include {
    opts.include = append(opts.include, regexp.MustCompile(r))
  }
  for _, r := range opts.Exclude {
    opts.exclude = append(opts.exclude, regexp.MustCompile(r))
  }
  for _, r := range opts.Files {
    opts.files = append(opts.files, regexp.MustCompile(r))
  }

  err = filepath.Walk(dir, getWatchDirs)
  if err != nil {
    log.Fatal(err)
  }

  watcher()
}
