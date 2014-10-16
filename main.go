// helps in developing the application by having
// source code change detection, recompiliation,
// and rerunning.
package main

import (
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
	"syscall"
	"time"

	"github.com/codegangsta/cli"
	"github.com/howeyc/fsnotify"
	"github.com/kballard/go-shellquote"
)

var (
	wpaths       = make(map[string]string)
	include      []*regexp.Regexp
	exclude      []*regexp.Regexp
	includeFiles []*regexp.Regexp
	excludeFiles []*regexp.Regexp
)

// run the program
func run(c *cli.Context, cmderr chan error) (*exec.Cmd, error) {
	if len(c.Args()) > 0 {
		log.Printf("Running program...\n")

		args := shellquote.Join(c.Args()...)
		if len(args) > 2 {
			if args[0] == '\'' || args[0] == '"' {
				args = args[1:]
			}
			if args[len(args)-1] == '\'' || args[len(args)-1] == '"' {
				args = args[:len(args)-1]
			}
		}

		cmd := exec.Command(os.ExpandEnv(c.String("shell")), "-c", args)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		err := cmd.Start()
		if err != nil {
			return nil, err
		}

		// Wait for the program and send the error value
		// on the channel. We use this later to determine
		// if a program has closed on its own and whether we
		go func() {
			cmderr <- cmd.Wait()
		}()

		return cmd, nil
	} else {
		log.Println("Detected code change")
	}

	return nil, nil
}

// shouldRerun returns true if we should rerun the program
// because `name` changed
func shouldRerun(name string) (ret bool) {
	ret = false

	// um... should be configurable?
	ret = !strings.HasPrefix(filepath.Base(name), ".")
	if !ret {
		return
	}

	for _, r := range excludeFiles {
		if r.MatchString(name) {
			ret = false
			return
		}
	}

	for _, r := range includeFiles {
		if r.MatchString(name) {
			ret = true
			return
		}
	}

	return false
}

func watcher(c *cli.Context) {
	log.Println("Running watcher")

	var wg sync.WaitGroup
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}

	cmderr := make(chan error)
	wg.Add(1)
	go func() {
		defer wg.Done()
		cmd, err := run(c, cmderr)
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
							// We use select here to determine if the
							// program has closed. If we have a value
							// on the cmderr channel, then the program
							// has already closed and we don't need to kil
							// it.
							select {
							case e := <-cmderr:
								if e != nil {
									log.Println(e)
								}
							default:
								log.Printf("Killing program...\n")
								cmd.Process.Signal(syscall.SIGINT)
								<-cmderr
							}
						}

						cmd, err = run(c, cmderr)
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

// find where on the filesystem a package is
func which(pkg string, location string) string {
	for _, top := range strings.Split(os.Getenv("GOPATH"), ":") {
		dir := top + "/" + location + "/" + pkg
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

// shouldWatch determines if we should watch the given
// path based on the include and exclude regexps.
func shouldWatch(path string) bool {
	for _, r := range exclude {
		if r.MatchString(path) {
			log.Println("exclude:", path)
			return false
		}
	}

	for _, r := range include {
		if r.MatchString(path) {
			return true
		}
	}

	return false
}

// getWatchDirs will add path to the watched dirs if it is a directory,
// or call getWatchDirsFromFile if it's a file.
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

// getWatchDirsFromFile finds all the watch directories from the
// imports of the file
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

		wpath := which(path, "src")

		if wpath != "" {
			if shouldWatch(wpath) {
				wpaths[wpath] = wpath
			}
		}
	}

	return nil
}

// Called when you run "devrun watch"
func cmdWatchAction(c *cli.Context) {
	var err error

	// build regexps
	for _, r := range c.StringSlice("include") {
		include = append(include, regexp.MustCompile(r))
	}
	for _, r := range c.StringSlice("exclude") {
		exclude = append(exclude, regexp.MustCompile(r))
	}
	for _, r := range c.StringSlice("include-files") {
		includeFiles = append(includeFiles, regexp.MustCompile(r))
	}
	for _, r := range c.StringSlice("exclude-files") {
		excludeFiles = append(excludeFiles, regexp.MustCompile(r))
	}

	for _, d := range c.StringSlice("dir") {
		err = filepath.Walk(d, getWatchDirs)
		if err != nil {
			log.Fatal(err)
		}
	}

	watcher(c)
}

func main() {
	app := cli.NewApp()
	app.Name = "devrun"
	app.Usage = "rebuild/rerun on source change"
	app.Commands = []cli.Command{
		{
			Name:   "watch",
			Usage:  "watches a repository for code changes. runs a specified command",
			Action: cmdWatchAction,
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  "shell",
					Value: "${SHELL}",
					Usage: "shell to use (defaults to the env variable SHELL)",
				},
				cli.StringSliceFlag{
					Name:  "dir",
					Value: &cli.StringSlice{"./"},
					Usage: "The directory(s) where to watch and scan for dependencies"},

				cli.StringSliceFlag{
					Name:  "include",
					Value: &cli.StringSlice{".*"},
					Usage: "Regexp of dirs to include for watching.",
				},
				cli.StringSliceFlag{
					Name:  "exclude",
					Value: &cli.StringSlice{`^\.*$`},
					Usage: `Regexp of dirs to exclude from watching.`,
				},
				cli.StringSliceFlag{
					Name:  "include-files",
					Value: &cli.StringSlice{`^(.*\.go)$`},
					Usage: `Regexp of files that, if changed, will cause a rerun.`,
				},
				cli.StringSliceFlag{
					Name:  "exclude-files",
					Value: &cli.StringSlice{`^\.*$`},
					Usage: `Regexp of files that, if changed, will not cause a rerun.`,
				},
			},
		},
	}

	app.Run(os.Args)
}
