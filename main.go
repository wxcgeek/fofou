// This code is in Public Domain. Take all the code you want, I'll just write more.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"runtime"
	"time"

	"github.com/coyove/common/rand"
	"github.com/coyove/fofou/server"
)

var (
	httpAddr     = flag.String("addr", ":5010", "HTTP server address")
	makeID       = flag.String("make", "", "Make an ID")
	inProduction = flag.String("production", "", "production domain")
	forum        *server.Forum
)

// NewForum creates new forum
func NewForum(config *server.ForumConfig, logger *server.Logger) *server.Forum {
	topbarBuf, _ := ioutil.ReadFile("data/topbar.html")
	forum := &server.Forum{
		ForumConfig: config,
		TopbarHTML:  string(topbarBuf),
		Logger:      logger,
	}

	start := time.Now()
	forum.Store = server.NewStore("data/main.txt", config.MaxLiveTopics, logger)

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

func main() {
	os.MkdirAll("data/archive", 0755)
	os.MkdirAll("data/images", 0755)
	runtime.GOMAXPROCS(runtime.NumCPU())

	flag.Parse()
	useStdout := *inProduction == ""

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
	if config.Salt == "" {
		config.Salt = fmt.Sprintf("%x", rand.New().Fetch(16))
		buf, _ := json.Marshal(&config)
		ioutil.WriteFile(configPath, buf, 0755)
	}

	if *makeID != "" {
		u := server.User{}
		copy(u.ID[:], *makeID)
		server.SetUser(nil, u)
		return
	}

	forum = NewForum(&config, server.NewLogger(1024, 1024, useStdout))
	srv := initHTTPServer()
	srv.Addr = *httpAddr
	forum.Notice("running on %s", srv.Addr)
	if err := srv.ListenAndServe(); err != nil {
		fmt.Printf("http.ListendAndServer() failed with %s\n", err)
	}
}
