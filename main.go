// This code is in Public Domain. Take all the code you want, I'll just write more.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/coyove/common/lru"
	"github.com/coyove/common/rand"
	"github.com/coyove/fofou/server"
)

var (
	httpAddr     = flag.String("addr", ":5010", "HTTP server address")
	makeID       = flag.String("make", "", "Make an ID")
	inProduction = flag.Bool("production", false, "go for production")
	forum        *server.Forum
	iq           *server.ImageQueue
)

func newForum(config *server.ForumConfig, logger *server.Logger) *server.Forum {
	forum := &server.Forum{
		ForumConfig: config,
		Logger:      logger,
	}

	start := time.Now()
	forum.Store = server.NewStore("data/main.txt", config.MaxLiveTopics, logger)
	forum.BadUsers = lru.NewCache(1024)
	forum.UUIDs = lru.NewCache(1024)

	go func() {
		for {
			time.Sleep(time.Second)
			if forum.Store.IsReady() {
				vt, p := forum.PostsCount()
				forum.Notice("%d topics, %d visible topics, %d posts", forum.TopicsCount(), vt, p)
				forum.Notice("loaded all in %.2fs", time.Now().Sub(start).Seconds())
				break
			}
		}
	}()
	return forum
}

func serveFileFromDir(w http.ResponseWriter, r *http.Request, dir, fileName string) {
	filePath := filepath.Join(dir, fileName)
	http.ServeFile(w, r, filePath)
}

// url: /s/*
func handleStatic(w http.ResponseWriter, r *http.Request) {
	file := r.URL.Path[len("/s/"):]
	serveFileFromDir(w, r, "static", file)
}

func handleImage(w http.ResponseWriter, r *http.Request) {
	file := r.URL.Path[len("/i/"):]
	file = "data/images/" + file

	if r.FormValue("thumb") == "1" {
		path := file + ".thumb.jpg"
		if _, err := os.Stat(path); err == nil {
			http.ServeFile(w, r, path)
			return
		}
		if !strings.HasSuffix(file, ".gif") {
			iq.Push(file)
		}
	}
	http.ServeFile(w, r, file)
}

// url: /robots.txt
func handleRobotsTxt(w http.ResponseWriter, r *http.Request) {
	serveFileFromDir(w, r, "static", "robots.txt")
}

func handleLogs(w http.ResponseWriter, r *http.Request) {
	if !forum.GetUser(r).IsAdmin() {
		w.WriteHeader(403)
		return
	}

	m := &runtime.MemStats{}
	runtime.ReadMemStats(m)

	model := struct {
		server.Forum
		Errors  []*server.TimestampedMsg
		Notices []*server.TimestampedMsg
		Header  *http.Header
		runtime.MemStats
	}{
		Forum:    *forum,
		MemStats: *m,
		Errors:   forum.GetErrors(),
		Notices:  forum.GetNotices(),
		Header:   &r.Header,
	}

	server.Render(w, server.TmplLogs, model)
}

func preHandle(fn func(http.ResponseWriter, *http.Request), footer bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !forum.IsReady() {
			w.Write([]byte(fmt.Sprintf("%v Booting... %.1f%%", time.Now().Format(time.RFC1123), forum.LoadingProgress()*100)))
			return
		}

		startTime := time.Now()
		fn(w, r)
		duration := time.Now().Sub(startTime)
		if footer {
			server.Render(w, server.TmplFooter, struct{ RenderTime int64 }{duration.Nanoseconds() / 1e6})
		}

		if duration.Seconds() > 0.1 {
			url := r.URL.Path
			if len(r.URL.RawQuery) > 0 {
				url = fmt.Sprintf("%s?%s", url, r.URL.RawQuery)
			}
			forum.Notice("%q took %f seconds to serve", url, duration.Seconds())
		}
	}
}
func main() {
	os.MkdirAll("data/archive", 0755)
	os.MkdirAll("data/images", 0755)
	runtime.GOMAXPROCS(runtime.NumCPU())

	flag.Parse()
	logger := server.NewLogger(1024, 1024, !*inProduction)

	var config server.ForumConfig
	var configPath = "data/main.json"
	b, err := ioutil.ReadFile(configPath)
	if err != nil {
		panic(err)
	}

	err = json.Unmarshal(b, &config)
	if err != nil {
		panic(err)
	}

	if config.MaxSubjectLen == 0 {
		config.MaxSubjectLen = 60
	}
	if config.MaxMessageLen == 0 {
		config.MaxMessageLen = 10000
	}
	if config.MinMessageLen == 0 {
		config.MinMessageLen = 3
	}

	configbuf, _ := json.MarshalIndent(&config, "", "  ")
	logger.Notice("%s", string(configbuf))

	if bytes.Equal(config.Salt[:], make([]byte, 32)) {
		copy(config.Salt[:], rand.New().Fetch(32))
		buf, _ := json.Marshal(&config)
		ioutil.WriteFile(configPath, buf, 0755)
	}

	if *makeID != "" {
		u := server.User{}
		copy(u.ID[:], *makeID)
		forum = &server.Forum{}
		forum.ForumConfig = &config
		forum.SetUser(nil, u)
		return
	}

	forum = newForum(&config, logger)
	iq = server.NewImageQueue(forum.Logger, 200)

	server.LoadTemplates(*inProduction)

	smux := &http.ServeMux{}
	smux.HandleFunc("/favicon.ico", http.NotFound)
	smux.HandleFunc("/robots.txt", handleRobotsTxt)
	smux.HandleFunc("/logs", preHandle(handleLogs, true))
	smux.HandleFunc("/s/", preHandle(handleStatic, false))
	smux.HandleFunc("/i/", preHandle(handleImage, false))
	smux.HandleFunc("/api", preHandle(handleNewPost, false))
	smux.HandleFunc("/list", preHandle(handleList, true))
	smux.HandleFunc("/rss.xml", preHandle(handleRSS, false))
	smux.HandleFunc("/t/", preHandle(handleTopic, true))
	smux.HandleFunc("/p/", preHandle(handleRawPost, false))
	smux.HandleFunc("/", preHandle(handleTopics, true))

	srv := &http.Server{Handler: smux}
	srv.Addr = *httpAddr
	forum.Notice("running on %s", srv.Addr)
	if err := srv.ListenAndServe(); err != nil {
		fmt.Printf("http.ListendAndServer() failed with %s\n", err)
	}
}
