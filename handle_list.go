// This code is in Public Domain. Take all the code you want, I'll just write more.
package main

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/coyove/fofou/server"
)

func handleList(w http.ResponseWriter, r *http.Request) {
	store := forum.Store
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
	}{Forum: *forum}

	if q == "" && qt == "" {
		server.Render(w, server.TmplPosts, model)
		return
	}

	query := server.Parse8Bytes(q)
	isAdmin := forum.GetUser(r).IsAdmin()
	maxTopics := 50

	if count := r.FormValue("count"); isAdmin && count != "" {
		maxTopics, _ = strconv.Atoi(count)
	}

	posts, total := store.GetPostsBy(query, qt, maxTopics, 1e8 /* 100ms */)
	isBlocked := store.IsBlocked(query)

	model.Forum = *forum
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
