// This code is in Public Domain. Take all the code you want, I'll just write more.
package main

import (
	"fmt"
	"net/http"
	"time"

	atom "github.com/kjk/atomgenerator"
)

func buildForumURL(r *http.Request, forum *Forum) string {
	return fmt.Sprintf("http://%s/%s", r.Host, forum.ForumUrl)
}

func buildTopicURL(r *http.Request, forum *Forum, p *Post) string {
	return fmt.Sprintf("http://%s/%s/topic?id=%d&post=%d", r.Host, forum.ForumUrl, p.Topic.ID, p.Id)
}

func buildTopicID(r *http.Request, forum *Forum, p *Post) string {
	pubDateStr := time.Unix(int64(p.CreatedOn), 0).Format("2006-01-02")
	url := fmt.Sprintf("/%s/topic?id=%d&post=%d", forum.ForumUrl, p.Topic.ID, p.Id)
	return fmt.Sprintf("tag:%s,%s:%s", r.Host, pubDateStr, url)
}

func handleRss2(w http.ResponseWriter, r *http.Request, all bool) {
	forum := mustGetForum(w, r)
	if forum == nil {
		return
	}
	var posts []*Post
	if all {
		posts = forum.Store.GetRecentPosts(25)
	} else {
		topics, _ := forum.Store.GetTopics(25, 0, false)
		posts = make([]*Post, len(topics), len(topics))
		for i, t := range topics {
			posts[i] = &t.Posts[0]
		}
	}

	pubTime := time.Now()
	if len(posts) > 0 {
		pubTime = time.Unix(int64(posts[0].CreatedOn), 0)
	}

	feed := &atom.Feed{
		Title:   forum.Title,
		Link:    buildForumURL(r, forum),
		PubDate: pubTime,
	}

	for _, p := range posts {
		msgStr := msgToHtml(bytesToPlane0String(p.Message))
		//id := fmt.Sprintf("tag:forums.fofou.org,1999:%s-topic-%d-post-%d", forum.ForumUrl, p.Topic.Id, p.Id)
		e := &atom.Entry{
			Id:      buildTopicID(r, forum, p),
			Title:   p.Topic.Subject,
			PubDate: time.Unix(int64(p.CreatedOn), 0),
			Link:    buildTopicURL(r, forum, p),
			Content: msgStr,
		}
		feed.AddEntry(e)
	}

	s, err := feed.GenXml()
	if err != nil {
		s = []byte("Failed to generate XML feed")
	}

	w.Write(s)
}

// url: /{forum}/rss
func handleRss(w http.ResponseWriter, r *http.Request) {
	handleRss2(w, r, false)
}

// url: /{forum}/rssall
func handleRssAll(w http.ResponseWriter, r *http.Request) {
	handleRss2(w, r, true)
}
