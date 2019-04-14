// This code is in Public Domain. Take all the code you want, I'll just write more.
package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/coyove/fofou/common"
	"github.com/coyove/fofou/server"
)

func List(w http.ResponseWriter, r *http.Request) {
	store := common.Kforum.Store
	q := r.FormValue("q")
	qt := r.FormValue("qt")

	model := struct {
		server.Forum
		server.Topic
		TotalCount int
		IsAdmin    bool
		Query      string
		QueryText  string
		Blocked    map[string]bool
		IsBlocked  bool
	}{Forum: *common.Kforum}

	if q == "" && qt == "" {
		server.Render(w, server.TmplPosts, model)
		return
	}

	query := server.Parse8Bytes(q)
	isAdmin := common.Kforum.GetUser(r).CanModerate()
	maxTopics := 50

	if count := r.FormValue("count"); isAdmin && count != "" {
		maxTopics, _ = strconv.Atoi(count)
	}

	posts, total := store.GetPostsBy(query, qt, maxTopics, int64(common.Kforum.SearchTimeout)*1e6)
	isBlocked := store.IsBlocked(query)

	for i := range posts {
		posts[i].T_SetStatus(server.POST_T_ISREF)
	}

	model.Topic = server.Topic{
		Posts:     posts,
		Subject:   fmt.Sprintf("%s: %x", q, query),
		T_IsAdmin: isAdmin,
	}
	model.TotalCount = total
	model.IsAdmin = isAdmin
	model.IsBlocked = isBlocked
	model.Query = q
	model.QueryText = qt

	server.Render(w, server.TmplPosts, model)
}

func RSS(w http.ResponseWriter, r *http.Request) {
	xml := []string{
		`<?xml version="1.0" encoding="UTF-8"?>`,
		`<rss version="2.0"><channel>`,
		`<title>`, common.Kforum.Title, `</title>`,
		`<pubDate>`, time.Now().Format(time.RFC1123Z), `</pubDate>`,
		`<link>`, common.Kforum.URL, `</link>`,
	}

	topics := common.Kforum.GetTopics(0, 20, common.TopicFilter1, server.DefaultTopicMapper)
	for _, g := range topics {
		var message string
		if len(g.Posts) > 0 {
			message = g.Posts[0].MessageHTML()
		}

		xml = append(xml,
			`<item>`,
			`<title>`, g.Subject, `</title>`,
			`<pubDate>`, time.Unix(int64(g.CreatedAt), 0).Format(time.RFC1123Z), `</pubDate>`,
			`<link>`, common.Kforum.URL, "/t/", strconv.FormatUint(uint64(g.ID), 10), `</link>`,
			`<description>`, `<![CDATA[`, message, `]]>`, `</description>`,
			`</item>`,
		)
	}

	xml = append(xml, "</channel></rss>")

	w.Header().Add("Content-Type", "application/xml")
	w.Write([]byte(strings.Join(xml, "")))
}
