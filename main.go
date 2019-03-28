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
	"strconv"
	"strings"
	"time"

	"github.com/coyove/common/lru"
	"github.com/coyove/common/rand"
	"github.com/coyove/fofou/server"
)

const (
	DATA_IMAGES = "data/images/"
	DATA_MAIN   = "data/main.txt"
	DATA_CONFIG = "data/main.json"
)

var (
	httpAddr     = flag.String("addr", ":5010", "HTTP server address")
	makeID       = flag.String("make", "", "Make an ID")
	snapshot     = flag.String("ss", "", "Make snapshot of main.txt")
	inProduction = flag.Bool("prod", false, "go for production")
	forum        *server.Forum
	iq           *server.ImageQueue
	throtIPID    *lru.Cache
	badUsers     *lru.Cache
	uuids        *lru.Cache
	dirServer    http.Handler
)

func atoi(v string) (byte, int8, uint16, int16, uint32, int32, uint64, int64, uint, int) {
	i, _ := strconv.ParseUint(v, 10, 64)
	return byte(i), int8(i), uint16(i), int16(i), uint32(i), int32(i), i, int64(i), uint(i), int(i)
}

func newForum(config *server.ForumConfig, logger *server.Logger) *server.Forum {
	forum := &server.Forum{
		ForumConfig: config,
		Logger:      logger,
	}

	start := time.Now()
	forum.Store = server.NewStore(DATA_MAIN, config.Salt, config.MaxLiveTopics, logger)
	badUsers = lru.NewCache(1024)
	uuids = lru.NewCache(1024)
	throtIPID = lru.NewCache(256)

	go func() {
		for {
			time.Sleep(100 * time.Millisecond)
			if forum.Store.IsReady() {
				vt, p := forum.PostsCount()
				forum.Notice("%d topics, %d live topics = %d, %d posts", forum.TopicsCount(), forum.LiveTopicsNum, vt, p)
				forum.Notice("loaded all in %.2fs", time.Now().Sub(start).Seconds())

				if *snapshot != "" {
					server.SnapshotStore(*snapshot, forum.Store)
					os.Exit(0)
				}

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
	file = DATA_IMAGES + file

	if r.FormValue("thumb") == "1" {
		path := file + ".thumb.jpg"
		if _, err := os.Stat(path); err == nil {
			http.ServeFile(w, r, path)
			return
		}
		iq.Push(file)
	}
	http.ServeFile(w, r, file)
}

func handleImageBrowser(w http.ResponseWriter, r *http.Request) {
	if !forum.GetUser(r).Can(server.PERM_ADMIN) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	dirServer.ServeHTTP(w, r)
}

func handleHelp(w http.ResponseWriter, r *http.Request) {
	path := "data/main.txt.snapshot"
	if r.RequestURI == "/data.bin" {
		http.ServeFile(w, r, path)
		return
	}
	fi, _ := os.Stat(path)
	p := struct {
		server.Forum
		DataBinSize uint64
		DataBinTime string
	}{}
	p.Forum = *forum
	if fi != nil {
		p.DataBinSize = uint64(fi.Size())
		p.DataBinTime = fi.ModTime().Format(time.RFC1123)
	}
	server.Render(w, server.TmplHelp, p)
}

// url: /robots.txt
func handleRobotsTxt(w http.ResponseWriter, r *http.Request) {
	serveFileFromDir(w, r, "static", "robots.txt")
}

func handleCookie(w http.ResponseWriter, r *http.Request) {
	if m := r.FormValue("makeid"); m != "" {
		if !forum.GetUser(r).Can(server.PERM_ADMIN) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		u, parts := server.User{}, strings.Split(m, ",")
		copy(u.ID[:], parts[0])
		u.M, _, _, _, _, _, _, _, _, _ = atoi(parts[1])
		if len(parts) > 2 {
			_, _, _, _, u.N, _, _, _, _, _ = atoi(parts[2])
		}
		w.Write([]byte(forum.SetUser(nil, u)))
		return
	}
	if m := r.FormValue("uid"); m != "" {
		cookie := &http.Cookie{
			Name:    "uid",
			Value:   m,
			Path:    "/",
			Expires: time.Now().AddDate(1, 0, 0),
		}
		http.SetCookie(w, cookie)
		http.Redirect(w, r, "/", 302)
		return
	}

	uid, _ := r.Cookie("uid")
	if uid != nil {
		w.Write([]byte(uid.Value))
	} else {
		w.Write([]byte("no cookie"))
	}
}

func handleLogs(w http.ResponseWriter, r *http.Request) {
	if !forum.GetUser(r).Can(server.PERM_ADMIN) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	m := &runtime.MemStats{}
	runtime.ReadMemStats(m)

	model := struct {
		server.Forum
		Errors  []*server.TimestampedMsg
		Notices []*server.TimestampedMsg
		Header  *http.Header
		IP      string
		IQLen   int
		runtime.MemStats
	}{
		Forum:    *forum,
		MemStats: *m,
		Errors:   forum.GetErrors(),
		Notices:  forum.GetNotices(),
		Header:   &r.Header,
		IQLen:    iq.Len(),
	}
	model.IP, _ = server.Format8Bytes(getIPAddress(r))
	server.Render(w, server.TmplLogs, model)
}

func preHandle(fn func(http.ResponseWriter, *http.Request), footer bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !forum.IsReady() {
			w.Write([]byte(fmt.Sprintf("%v Booting... %.1f%%", time.Now().Format(time.RFC1123), forum.LoadingProgress()*100)))
			return
		}
		if footer {
			w = &server.ResponseWriterWrapper{w, http.StatusOK}
		}

		startTime := time.Now()
		fn(w, r)
		duration := time.Since(startTime)

		if footer && w.(*server.ResponseWriterWrapper).Code == http.StatusOK {
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
	os.MkdirAll(DATA_IMAGES, 0755)
	runtime.GOMAXPROCS(runtime.NumCPU())

	flag.Parse()
	logger := server.NewLogger(1024, 1024, !*inProduction)

	var config server.ForumConfig
	b, err := ioutil.ReadFile(DATA_CONFIG)
	if err != nil {
		panic(err)
	}

	err = json.Unmarshal(b, &config)
	if err != nil {
		panic(err)
	}

	checkInt := func(i *int, v int) { *i = int(^(^uint32(*i-1)>>31)&1)*v + *i }
	checkInt(&config.MaxSubjectLen, 60)
	checkInt(&config.MaxMessageLen, 10000)
	checkInt(&config.MinMessageLen, 3)
	checkInt(&config.SearchTimeout, 100)
	checkInt(&config.MaxImageSize, 4)
	checkInt(&config.Cooldown, 2)
	checkInt(&config.PostsPerPage, 20)
	checkInt(&config.TopicsPerPage, 15)

	if bytes.Equal(config.Salt[:], make([]byte, 16)) {
		copy(config.Salt[:], rand.New().Fetch(16))
	}

	configbuf, _ := json.MarshalIndent(&config, "", "  ")
	logger.Notice("%s", string(configbuf))
	ioutil.WriteFile(DATA_CONFIG, configbuf, 0755)

	if *makeID != "" {
		u, parts := server.User{}, strings.Split(*makeID, ",")
		copy(u.ID[:], parts[0])
		m, _ := strconv.Atoi(parts[1])
		u.M = byte(m)

		forum = &server.Forum{}
		forum.ForumConfig = &config
		forum.SetUser(nil, u)
		return
	}

	forum = newForum(&config, logger)
	iq = server.NewImageQueue(forum.Logger, 200, runtime.NumCPU())

	server.LoadTemplates(*inProduction)

	smux := &http.ServeMux{}
	dirServer = http.StripPrefix("/browse/", http.FileServer(http.Dir(DATA_IMAGES)))
	smux.HandleFunc("/favicon.ico", http.NotFound)
	smux.HandleFunc("/robots.txt", handleRobotsTxt)
	smux.HandleFunc("/mod", preHandle(handleLogs, true))
	smux.HandleFunc("/cookie", preHandle(handleCookie, false))
	smux.HandleFunc("/s/", preHandle(handleStatic, false))
	smux.HandleFunc("/status", preHandle(handleHelp, true))
	smux.HandleFunc("/i/", preHandle(handleImage, false))
	smux.HandleFunc("/browse/", preHandle(handleImageBrowser, false))
	smux.HandleFunc("/api", preHandle(handleNewPost, false))
	smux.HandleFunc("/list", preHandle(handleList, true))
	smux.HandleFunc("/rss.xml", preHandle(handleRSS, false))
	smux.HandleFunc("/data.bin", preHandle(handleHelp, false))
	smux.HandleFunc("/t/", preHandle(handleTopic, true))
	smux.HandleFunc("/p/", preHandle(handleRawPost, false))
	smux.HandleFunc("/", preHandle(handleTopics, true))

	srv := &http.Server{Handler: smux}
	srv.Addr = *httpAddr
	forum.Notice("running on %s", srv.Addr)

	go func() {
		for {
			if *inProduction {
				time.Sleep(time.Hour * 6)
			} else {
				time.Sleep(time.Minute)
			}

			start := time.Now()
			forum.Store.Dup()
			forum.Notice("dup: %.2fs", time.Since(start).Seconds())
		}
	}()

	if err := srv.ListenAndServe(); err != nil {
		fmt.Printf("http.ListendAndServer() failed with %s\n", err)
	}
}
