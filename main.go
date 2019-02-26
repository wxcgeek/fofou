// This code is in Public Domain. Take all the code you want, I'll just write more.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"path/filepath"
	"runtime"
	"time"

	"github.com/kjk/u"
)

var (
	httpAddr     = flag.String("addr", ":5010", "HTTP server address")
	inProduction = flag.String("production", "", "production domain")
	cookieName   = "ckie"
)

var (
	logger        *ServerLogger
	forum         *Forum
	alwaysLogTime = true
)

// ForumConfig is a static configuration of a single forum
type ForumConfig struct {
	Title          string
	TopbarHTML     string
	AdminLoginName string
	NoMoreNewUsers bool
	MaxLiveTopics  int
	MaxSubjectLen  int
	MaxMessageLen  int
	MinMessageLen  int
	Recaptcha      string
}

// Forum describes forum
type Forum struct {
	*ForumConfig
	Store *Store
}

func (f *Forum) IsAdmin(id string) bool { return f.AdminLoginName == id }

// NewForum creates new forum
func NewForum(config *ForumConfig) *Forum {
	sidebarTmplPath := filepath.Join("data/topbar.html")
	panicif(!u.PathExists(sidebarTmplPath), "topbar template %s doesn't exist", sidebarTmplPath)
	topbarBuf, _ := ioutil.ReadFile(sidebarTmplPath)

	forum := &Forum{ForumConfig: config}
	forum.TopbarHTML = string(topbarBuf)

	store := NewStore("data/main.txt")
	a, b := store.PostsCount()
	logger.Noticef("%d topics, %d visible topics, %d posts", store.TopicsCount(), a, b)
	forum.Store = store
	store.MaxLiveTopics = forum.MaxLiveTopics
	return forum
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
	u.CreateDirIfNotExists("data")
	u.CreateDirIfNotExists("data/archive")

	runtime.GOMAXPROCS(runtime.NumCPU())
	flag.Parse()

	if *inProduction != "" {
		alwaysLogTime = false
	}

	useStdout := *inProduction == ""
	logger = NewServerLogger(256, 256, useStdout)

	rand.Seed(time.Now().UnixNano())

	var config ForumConfig
	b, err := ioutil.ReadFile("data/main.json")
	panicif(err != nil, err)

	err = json.Unmarshal(b, &config)
	panicif(err != nil, err)

	if config.MaxSubjectLen == 0 {
		config.MaxSubjectLen = 60
	}
	if config.MaxMessageLen == 0 {
		config.MaxMessageLen = 10000
	}
	if config.MinMessageLen == 0 {
		config.MinMessageLen = 3
	}

	start := time.Now()
	forum = NewForum(&config)
	fmt.Printf("loaded all in %.2fs\n", time.Now().Sub(start).Seconds())

	srv := initHTTPServer()
	srv.Addr = *httpAddr
	logger.Noticef("running on %s\n", srv.Addr)
	if err := srv.ListenAndServe(); err != nil {
		fmt.Printf("http.ListendAndServer() failed with %s\n", err)
	}
}
