package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type forbidden struct{}

type fileSystem struct {
	fs http.FileSystem
}

func (fs fileSystem) Open(name string) (http.File, error) {
	f, err := fs.fs.Open(name)
	if err != nil {
		return nil, err
	}
	return file{f}, nil
}

type file struct {
	http.File
}

func (f file) Readdir(count int) ([]os.FileInfo, error) {
	if dirListing {
		return f.File.Readdir(count)
	}
	panic(forbidden{})
}

func (f file) Stat() (os.FileInfo, error) {
	fi, err := f.File.Stat()
	if err != nil {
		return nil, err
	}
	return fileInfo{fi}, nil
}

type fileInfo struct {
	os.FileInfo
}

func (fi fileInfo) ModTime() time.Time {
	if fi.IsDir() || fi.Name() == "index.html" {
		// Zero time makes net/http omit the Last-Modified header
		return time.Time{}
	}
	return fi.FileInfo.ModTime()
}

var (
	configPath string
	dirListing bool
	dirs       map[string]string
)

func init() {
	flag.StringVar(&configPath, "c", filepath.Join(must(os.UserHomeDir()), "hf.conf"), "config file")
	flag.BoolVar(&dirListing, "d", false, "enable directory listing")
	flag.Parse()

	dirs = make(map[string]string)
	config, err := os.Open(configPath)
	if errors.Is(err, fs.ErrNotExist) {
		dirs = map[string]string{":8000": "."}
		return
	}
	check(err)
	defer config.Close()

	s := bufio.NewScanner(config)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		addr, dir, f := strings.Cut(line, " ")
		if !f {
			fmt.Fprintln(os.Stderr, "bad config file")
			os.Exit(1)
		}
		addr = strings.TrimSpace(addr)
		dir = strings.TrimSpace(dir)
		dirs[addr] = dir
	}
	check(s.Err())
}

func main() {
	wg := sync.WaitGroup{}

	for addr, dir := range dirs {
		addr, dir := addr, dir
		wg.Add(1)

		go func() {
			s := &http.Server{
				Addr:    addr,
				Handler: recoverer(http.FileServer(fileSystem{http.Dir(dir)})),
			}
			check(s.ListenAndServe())
			wg.Done()
		}()
	}
	wg.Wait()
}

func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if (recover() == forbidden{}) {
				http.Error(w, "Forbidden", http.StatusForbidden)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func must[T any](t T, err error) T {
	check(err)
	return t
}
