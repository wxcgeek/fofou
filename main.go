// This code is in Public Domain. Take all the code you want, I'll just write more.
package main

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/kjk/u"
	"golang.org/x/crypto/acme/autocert"
)

var (
	httpAddr     = flag.String("addr", ":5010", "HTTP server address")
	inProduction = flag.String("production", "", "production domain")
	cookieName   = "ckie"
)

var (
	forums   = make([]*ForumConfig, 0)
	logger   *ServerLogger
	dataDir  string
	appState = AppState{
		Users:  make([]*User, 0),
		Forums: make([]*Forum, 0),
	}
	alwaysLogTime = true
)

// ForumConfig is a static configuration of a single forum
type ForumConfig struct {
	Title          string
	TopbarHTML     string
	ForumUrl       string
	DataDir        string
	AdminLoginName string
	Disabled       bool
	MaxLiveTopics  int
	BannedWords    *[]string
	Recaptcha      string
}

// User describes a user
type User struct {
	Login string
}

// Forum describes forum
type Forum struct {
	ForumConfig
	Store *Store
}

func (f *Forum) IsAdmin(id string) bool { return f.AdminLoginName == id }

// AppState describes state of the app
type AppState struct {
	Users  []*User
	Forums []*Forum
}

func getDataDir() string {
	u.CreateDirIfNotExists("data/forum")
	u.CreateDirIfNotExists("data/archive")
	dataDir = "data"
	return dataDir
}

// NewForum creates new forum
func NewForum(config *ForumConfig) *Forum {
	forum := &Forum{ForumConfig: *config}
	sidebarTmplPath := filepath.Join("forums", fmt.Sprintf("%s_topbar.html", forum.ForumUrl))
	panicif(!u.PathExists(sidebarTmplPath), "topbar template %s for forum %s doesn't exist", sidebarTmplPath, forum.ForumUrl)

	topbarBuf, _ := ioutil.ReadFile(sidebarTmplPath)
	forum.TopbarHTML = string(topbarBuf)

	store := NewStore(getDataDir(), config.DataDir)
	a, b := store.PostsCount()
	logger.Noticef("%d topics, %d visible topics, %d posts in forum %q", store.TopicsCount(), a, b, config.ForumUrl)
	forum.Store = store
	store.MaxLiveTopics = forum.MaxLiveTopics
	return forum
}

func findForum(forumURL string) *Forum {
	for _, f := range appState.Forums {
		if f.ForumUrl == forumURL {
			return f
		}
	}
	return nil
}

func forumAlreadyExists(siteURL string) bool {
	return nil != findForum(siteURL)
}

func forumInvalidField(forum *Forum) string {
	forum.Title = strings.TrimSpace(forum.Title)
	if forum.Title == "" {
		return "Title"
	}
	if forum.ForumUrl == "" {
		return "ForumUrl"
	}
	if forum.DataDir == "" {
		return "DataDir"
	}
	if forum.AdminLoginName == "" {
		return "AdminLoginName"
	}
	return ""
}

func addForum(forum *Forum) error {
	if invalidField := forumInvalidField(forum); invalidField != "" {
		return fmt.Errorf("Forum has invalid field %q", invalidField)
	}
	if forumAlreadyExists(forum.ForumUrl) {
		return errors.New("Forum already exists")
	}
	appState.Forums = append(appState.Forums, forum)
	return nil
}

// reads forums/*_config.json files
func readForumConfigs(configDir string) error {
	pat := filepath.Join(configDir, "*.json")
	files, err := filepath.Glob(pat)
	if err != nil {
		return err
	}
	if files == nil {
		return errors.New("no forums configured")
	}
	for _, configFile := range files {
		var forum ForumConfig
		b, err := ioutil.ReadFile(configFile)
		if err != nil {
			return err
		}
		err = json.Unmarshal(b, &forum)
		if err != nil {
			return err
		}
		if !forum.Disabled {
			forums = append(forums, &forum)
		}
	}
	if len(forums) == 0 {
		return errors.New("all forums are disabled")
	}
	return nil
}

func getReferer(r *http.Request) string {
	return r.Header.Get("Referer")
}

func makeTimingHandler(fn func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		startTime := time.Now()
		fn(w, r)
		duration := time.Now().Sub(startTime)
		// log urls that take long time to generate i.e. over 1 sec in production
		// or over 0.1 sec in dev
		shouldLog := duration.Seconds() > 1.0
		if alwaysLogTime && duration.Seconds() > 0.1 {
			shouldLog = true
		}
		if shouldLog {
			url := r.URL.Path
			if len(r.URL.RawQuery) > 0 {
				url = fmt.Sprintf("%s?%s", url, r.URL.RawQuery)
			}
			logger.Noticef("%q took %f seconds to serve", url, duration.Seconds())
		}
	}
}

func main() {
	// set number of goroutines to number of cpus, but capped at 4 since
	// I don't expect this to be heavily trafficed website
	ncpu := runtime.NumCPU()
	if ncpu > 4 {
		ncpu = 4
	}
	runtime.GOMAXPROCS(ncpu)
	flag.Parse()

	if *inProduction != "" {
		alwaysLogTime = false
	}

	useStdout := *inProduction == ""
	logger = NewServerLogger(256, 256, useStdout)

	rand.Seed(time.Now().UnixNano())

	if err := readForumConfigs("forums"); err != nil {
		log.Fatalf("failed to read forum configs, err: %s", err)
	}

	for _, forumData := range forums {
		start := time.Now()
		f := NewForum(forumData)
		if err := addForum(f); err != nil {
			log.Fatalf("failed to add the forum: %s, err: %s\n", f.Title, err)
		} else {
			fmt.Printf("add forum %s in %.2fs\n", f.ForumUrl, time.Now().Sub(start).Seconds())
		}
	}

	if len(appState.Forums) == 0 {
		log.Fatalf("no forums defined in config.json")
	}

	if *inProduction != "" {
		m := autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(*inProduction),
		}
		srv := initHTTPServer()
		srv.Addr = ":443"
		srv.TLSConfig = &tls.Config{GetCertificate: m.GetCertificate}
		logger.Noticef("running HTTPS on %s, production domain: %s\n", srv.Addr, *inProduction)
		go func() {
			srv.ListenAndServeTLS("", "")
		}()
	}

	srv := initHTTPServer()
	srv.Addr = *httpAddr
	logger.Noticef("running on %s\n", srv.Addr)
	if err := srv.ListenAndServe(); err != nil {
		fmt.Printf("http.ListendAndServer() failed with %s\n", err)
	}
}
