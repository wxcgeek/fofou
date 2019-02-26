// This code is in Public Domain. Take all the code you want, I'll just write more.
package main

import (
	"net/http"
	"strconv"
	"strings"
)

// url: /{forum}/postsBy?[user=${userNameInternal}][ip=${ipInternal}]
func handleList(forum *Forum, w http.ResponseWriter, r *http.Request) {
	store := forum.Store
	userInternal := strings.TrimSpace(r.FormValue("u"))
	ipAddrInternal := strings.TrimSpace(r.FormValue("ip"))
	if userInternal == "" && ipAddrInternal == "" {
		logger.Noticef("handlePostsBy(): missing both user and ip")
		http.Redirect(w, r, "/", 302)
		return
	}

	isAdmin := forum.IsAdmin(getSecureCookie(r))
	maxTopics := 50
	if count := r.FormValue("count"); isAdmin && count != "" {
		maxTopics, _ = strconv.Atoi(count)
	}

	var total int
	var posts []Post
	if userInternal != "" {
		posts, total = store.GetPostsByUserInternal(userInternal, maxTopics)
	} else {
		posts, total = store.GetPostsByIPInternal(ipAddrInternal, maxTopics)
	}

	ipBlocked, userBlocked := false, store.IsBlocked("u"+userInternal)
	if ipAddrInternal != "" {
		ipBlocked = store.IsBlocked("b" + ipAddrInternal)
	}

	model := struct {
		Forum
		Posts       []Post
		TotalCount  int
		IsAdmin     bool
		User        string
		IPAddr      string
		Blocked     map[string]string
		IPBlocked   bool
		UserBlocked bool
	}{
		Forum:       *forum,
		Posts:       posts,
		TotalCount:  total,
		IsAdmin:     isAdmin,
		User:        userInternal,
		IPAddr:      ipAddrInternal,
		UserBlocked: userBlocked,
		IPBlocked:   ipBlocked,
	}

	if isAdmin {
		model.Blocked = make(map[string]string)
		forum.Store.RLock()
		for k := range forum.Store.blocked {
			if k[0] == 'b' {
				model.Blocked[k[1:]] = k[1:]
			}
		}
		forum.Store.RUnlock()
	}

	ExecTemplate(w, tmplPosts, model)
}
