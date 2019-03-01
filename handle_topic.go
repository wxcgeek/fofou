// This code is in Public Domain. Take all the code you want, I'll just write more.
package main

import (
	"math"
	"net/http"
	"strconv"

	"github.com/coyove/fofou/server"
	"github.com/kjk/u"
)

// url: /{forum}/topic?id=${id}
func handleTopic(w http.ResponseWriter, r *http.Request) {
	topicID, _ := strconv.Atoi(r.URL.Path[len("/t/"):])
	topic := forum.Store.TopicByID(uint32(topicID))
	if topic.ID == 0 {
		path := forum.Store.BuildArchivePath(uint32(topicID))
		if u.PathExists(path) {
			var err error
			topic, err = LoadSingleTopicInStore(path)
			if err == nil {
				topic.Archived = true
				goto NEXT
			}
		}

		forum.Notice("handleTopic(): didn't find topic with id %d, referer: %q", topicID, r.Referer())
		http.Redirect(w, r, "/", 302)
		return
	}
NEXT:
	isAdmin := forum.IsAdmin(getUser(r).ID)
	if topic.IsDeleted() && !isAdmin {
		http.Redirect(w, r, "/", 302)
		return
	}

	pages := int(math.Ceil(float64(len(topic.Posts)) / float64(PostsPerPage)))
	p, _ := strconv.Atoi(r.FormValue("p"))
	if p < 1 {
		p = 1
	}
	if p > pages {
		p = pages
	}

	topic.T_TotalPosts = uint16(len(topic.Posts) - 1)
	topic.T_IsAdmin = isAdmin
	topic.Posts = topic.Posts[(p-1)*PostsPerPage : int(math.Min(float64(p*PostsPerPage), float64(len(topic.Posts))))]

	model := struct {
		*Forum
		Topic
		TopicID int
		Pages   int
		CurPage int
	}{
		Forum:   forum,
		Topic:   topic,
		TopicID: topicID,
		Pages:   pages,
		CurPage: p,
	}
	server.Render(w, server.TmplTopic, model)
}
