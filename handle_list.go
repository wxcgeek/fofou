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
	query := server.Parse8Bytes(q)
	isAdmin := server.GetUser(r).IsAdmin()
	maxTopics := 50

	if count := r.FormValue("count"); isAdmin && count != "" {
		maxTopics, _ = strconv.Atoi(count)
	}

	posts, total := store.GetPostsBy(query, maxTopics)
	isBlocked := store.IsBlocked(query)

	model := struct {
		server.Forum
		server.Topic
		TotalCount int
		IsAdmin    bool
		Query      string
		Blocked    map[string]bool
		IsBlocked  bool
	}{
		Forum: *forum,
		Topic: server.Topic{
			Posts:     posts,
			Subject:   fmt.Sprintf("%s: %x", q, query),
			T_IsAdmin: isAdmin,
		},
		TotalCount: total,
		IsAdmin:    isAdmin,
		IsBlocked:  isBlocked,
		Query:      q,
	}

	server.Render(w, server.TmplPosts, model)
}
