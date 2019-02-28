// This code is in Public Domain. Take all the code you want, I'll just write more.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"runtime"
	"time"

	"github.com/coyove/fofou/server"
	"github.com/kjk/u"
)

var (
	httpAddr      = flag.String("addr", ":5010", "HTTP server address")
	makeID        = flag.String("make", "", "Make an ID")
	inProduction  = flag.String("production", "", "production domain")
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
	*Store
}

func (f *Forum) IsAdmin(id [8]byte) bool { return id[0] == 'a' && id[1] == ':' }

// NewForum creates new forum
func NewForum(config *ForumConfig, logger *server.Logger) *Forum {
	forum := &Forum{ForumConfig: config}

	topbarBuf, _ := ioutil.ReadFile("data/topbar.html")
	forum.TopbarHTML = string(topbarBuf)

	forum.Store = NewStore("data/main.txt", config.MaxLiveTopics, logger)
	return forum
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
			forum.Notice("%q took %f seconds to serve", url, duration.Seconds())
		}
	}
}

func main() {
	u.CreateDirIfNotExists("data")
	u.CreateDirIfNotExists("data/archive")
	u.CreateDirIfNotExists("data/images")

	runtime.GOMAXPROCS(runtime.NumCPU())
	flag.Parse()

	if *inProduction != "" {
		alwaysLogTime = false
	}

	useStdout := *inProduction == ""

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

	if *makeID != "" {
		u := User{}
		copy(u.ID[:], *makeID)
		setUser(nil, u)
		return
	}

	start := time.Now()
	forum = NewForum(&config, server.NewLogger(256, 256, useStdout))
	vt, p := forum.PostsCount()
	forum.Notice("%d topics, %d visible topics, %d posts", forum.TopicsCount(), vt, p)
	forum.Notice("loaded all in %.2fs", time.Now().Sub(start).Seconds())

	srv := initHTTPServer()
	srv.Addr = *httpAddr
	forum.Notice("running on %s", srv.Addr)
	if err := srv.ListenAndServe(); err != nil {
		fmt.Printf("http.ListendAndServer() failed with %s\n", err)
	}
}
