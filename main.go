// This code is in Public Domain. Take all the code you want, I'll just write more.
package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/coyove/common/lru"
	"github.com/coyove/fofou/common"
	"github.com/coyove/fofou/handler"
	"github.com/coyove/fofou/server"
)

var (
	listen   = flag.String("addr", ":5010", "HTTP server address")
	makeID   = flag.String("make", "", "Make ID, format: ID,MASK")
	snapshot = flag.String("ss", "", "Make snapshot of main.txt")
	salt     = flag.String("s", "0123456789", "A secret string")
	inProd   = flag.Bool("prod", false, "Run server in production environment")
)

func newForum(logger *server.Logger) *server.Forum {
	forum := &server.Forum{Logger: logger}

	start := time.Now()
	forum.Store = server.NewStore(common.DATA_MAIN,
		(&server.ForumConfig{}).SetSalt(*salt),
		logger,
		func(store *server.Store) {
			forum.ForumConfig = &server.ForumConfig{}
			store.GetConfig(forum.ForumConfig)
			forum.ForumConfig.CorrectValues()
			forum.SetSalt(*salt)
			forum.RecaptchaSecret = os.Getenv("f2_secret")
			forum.RecaptchaToken = os.Getenv("f2_token")
			forum.Notice("recaptcha: token: %s, secret: %s", forum.RecaptchaToken, forum.RecaptchaSecret)
		})

	common.KbadUsers = lru.NewCache(1024)
	common.Kuuids = lru.NewCache(1024)
	common.KthrotIPID = lru.NewCache(256)

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

func preHandle(fn func(http.ResponseWriter, *http.Request), footer bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !common.Kforum.IsReady() {
			w.Write([]byte(fmt.Sprintf("%v Booting... %.1f%%", time.Now().Format(time.RFC1123), common.Kforum.LoadingProgress()*100)))
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
			common.Kforum.Notice("%q took %f seconds to serve", url, duration.Seconds())
		}
	}
}

func main() {
	os.MkdirAll(common.DATA_IMAGES, 0755)
	runtime.GOMAXPROCS(runtime.NumCPU())

	flag.Parse()
	logger := server.NewLogger(1024, 1024, !*inProd)

	if *makeID != "" {
		u, parts := server.User{}, strings.Split(*makeID, ",")
		copy(u.ID[:], parts[0])
		m, _ := strconv.Atoi(parts[1])
		u.M = byte(m)

		forum := &server.Forum{}
		forum.ForumConfig = &server.ForumConfig{}
		forum.ForumConfig.SetSalt(*salt)
		forum.SetUser(nil, u)
		return
	}

	common.Kforum = newForum(logger)
	common.Kiq = server.NewImageQueue(logger, 200, runtime.NumCPU())
	common.Kprod = *inProd

	server.LoadTemplates(*inProd)

	smux := &http.ServeMux{}
	common.KdirServer = http.StripPrefix("/browse/", http.FileServer(http.Dir(common.DATA_IMAGES)))
	smux.HandleFunc("/favicon.ico", http.NotFound)
	smux.HandleFunc("/robots.txt", handler.RobotsTxt)
	smux.HandleFunc("/mod", preHandle(handler.Mod, true))
	smux.HandleFunc("/cookie", preHandle(handler.Cookie, false))
	smux.HandleFunc("/s/", preHandle(handler.Static, false))
	smux.HandleFunc("/status", preHandle(handler.Help, true))
	smux.HandleFunc("/i/", preHandle(handler.Image, false))
	smux.HandleFunc("/browse/", preHandle(handler.ImageBrowser, false))
	smux.HandleFunc("/api", preHandle(handler.PostAPI, false))
	smux.HandleFunc("/list", preHandle(handler.List, true))
	smux.HandleFunc("/rss.xml", preHandle(handler.RSS, false))
	smux.HandleFunc("/data.bin", preHandle(handler.Help, false))
	smux.HandleFunc("/t/", preHandle(handler.Topic, true))
	smux.HandleFunc("/p/", preHandle(handler.Post, false))
	smux.HandleFunc("/tagged", preHandle(handler.Topics, true))
	smux.HandleFunc("/", preHandle(handler.Topics, true))

	srv := &http.Server{Handler: smux}
	srv.Addr = *listen
	logger.Notice("running on %s", srv.Addr)

	go func() {
		for {
			if *inProd {
				time.Sleep(time.Hour * 6)
			} else {
				time.Sleep(time.Minute)
			}

			start := time.Now()
			if err := common.Kforum.Store.Dup(common.DATA_MAIN + ".snapshot"); err != nil {
				logger.Error("dup error: %v", err)
			}
			logger.Notice("dup: %.2fs", time.Since(start).Seconds())
		}
	}()

	if err := srv.ListenAndServe(); err != nil {
		fmt.Printf("http.ListendAndServer() failed with %s\n", err)
	}
}
