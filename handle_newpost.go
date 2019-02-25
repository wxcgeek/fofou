// This code is in Public Domain. Take all the code you want, I'll just write more.
package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/coyove/common/rand"
	"github.com/coyove/common/session"
)

// ModelNewPost represents a new post
type ModelNewPost struct {
	Forum
	TopicID      int
	PrevCaptcha  string
	PrevSubject  string
	Token        string
	SubjectError bool
	MessageError bool
	TokenError   bool
	TopicLocked  bool
	PrevMessage  string
	NameClass    string
	PrevName     string
}

var randG = rand.New()

func isSubjectValid(subject string) bool {
	return len(subject) >= 6 && len(subject) <= 96
}

func isMsgValid(msg string, topic *Topic) bool {
	if len(msg) < 6 || len(msg) > 16*1024 {
		return false
	}
	// prevent duplicate posts within the topic
	if topic != nil {
		for _, p := range topic.Posts {
			if p.Message() == msg {
				return false
			}
		}
	}
	return true
}

// Request.RemoteAddress contains port, which we want to remove i.e.:
// "[::1]:58292" => "[::1]"
func ipAddrFromRemoteAddr(s string) string {
	idx := strings.LastIndex(s, ":")
	if idx == -1 {
		return s
	}
	return s[:idx]
}

func getIPAddress(r *http.Request) string {
	hdr := r.Header
	hdrRealIP := hdr.Get("X-Real-Ip")
	hdrForwardedFor := hdr.Get("X-Forwarded-For")
	if hdrRealIP == "" && hdrForwardedFor == "" {
		return ipAddrFromRemoteAddr(r.RemoteAddr)
	}
	if hdrForwardedFor != "" {
		// X-Forwarded-For is potentially a list of addresses separated with ","
		parts := strings.Split(hdrForwardedFor, ",")
		for i, p := range parts {
			parts[i] = strings.TrimSpace(p)
		}
		// TODO: should return first non-local address
		return parts[0]
	}
	return hdrRealIP
}

var badUserHTML = `
<html>
<head>
</head>

<body>
Internal problem 0xcc03fad detected ...
</body>
</html>
`

func isMessageBlocked(forum Forum, msg string) bool {
	bannedWords := forum.BannedWords
	if bannedWords != nil {
		for _, s := range *bannedWords {
			if strings.Index(msg, s) != -1 {
				return true
			}
		}
	}
	return false
}

func createNewPost(w http.ResponseWriter, r *http.Request, model *ModelNewPost, topic *Topic) {
	ipAddr := getIPAddress(r)
	ipAddrInternal := IPAddress(ipAddrToInternal(ipAddr))
	if model.Forum.Store.IsBlocked("b" + ipAddrInternal) {
		logger.Noticef("blocked a post from ip address %s (%s)", ipAddr, ipAddrInternal)
		w.Write([]byte(badUserHTML))
		return
	}

	if r.FormValue("Cancel") != "" {
		if tid := r.FormValue("tid"); tid != "" {
			http.Redirect(w, r, fmt.Sprintf("/%s?tid=%s", model.Forum.ForumUrl, tid), 302)
		} else {
			http.Redirect(w, r, fmt.Sprintf("/%s", model.Forum.ForumUrl), 302)
		}
		return
	}

	recaptcha := strings.TrimSpace(r.FormValue("g-recaptcha-response"))
	if recaptcha == "" {
		w.Write([]byte(badUserHTML))
		return
	}

	resp, err := (&http.Client{Timeout: time.Second * 5}).PostForm("https://www.recaptcha.net/recaptcha/api/siteverify", url.Values{
		"secret":   []string{model.Forum.Recaptcha},
		"response": []string{recaptcha},
	})
	if err != nil {
		logger.Errorf("recaptcha error: %v", err)
		w.WriteHeader(502)
		return
	}

	defer resp.Body.Close()
	buf, _ := ioutil.ReadAll(resp.Body)

	recaptchaResult := map[string]interface{}{}
	json.Unmarshal(buf, &recaptchaResult)

	if r, _ := recaptchaResult["success"].(bool); !r {
		logger.Errorf("recaptcha failed: %v", string(buf))
		w.Write([]byte(badUserHTML))
		return
	}

	// validate the fields
	subject := strings.TrimSpace(r.FormValue("Subject"))
	subject = strings.Replace(template.HTMLEscapeString(subject), "\n", "", -1)
	msg := strings.TrimSpace(r.FormValue("Message"))
	token := strings.TrimSpace(r.FormValue("Token"))

	if isMessageBlocked(model.Forum, msg) {
		logger.Notice("blocked a post of banned words: " + msg)
		w.Write([]byte(badUserHTML))
		return
	}

	model.PrevSubject = subject
	model.PrevMessage = msg

	if model.TopicID != 0 {
		model.PrevSubject = topic.Subject
	}

	ok := true
	if (model.TopicID == 0) && !isSubjectValid(subject) {
		model.SubjectError = true
		ok = false
	} else if !isMsgValid(msg, topic) {
		model.MessageError = true
		ok = false
	} else if topic != nil && topic.Locked {
		model.TopicLocked = true
		ok = false
	} else if !session.ConsumeString(token, ipAddrInternal) {
		model.TokenError = true
		ok = false
	}

	if !ok {
		ExecTemplate(w, tmplNewPost, model)
		return
	}

	userName := getSecureCookie(r)
	if userName == "" {
		userName = base64.URLEncoding.EncodeToString(randG.Fetch(6))
	}
	setSecureCookie(w, userName)

	if model.Forum.Store.IsBlocked("u" + userName) {
		logger.Noticef("blocked a post from user %s", userName)
		w.Write([]byte(badUserHTML))
		return
	}

	store := model.Forum.Store
	if topic == nil {
		if topicID, err := store.CreateNewTopic(subject, msg, userName, ipAddr); err != nil {
			logger.Errorf("createNewPost(): store.CreateNewPost() failed with %s", err)
		} else {
			http.Redirect(w, r, fmt.Sprintf("/%s?tid=%d", model.ForumUrl, topicID), 302)
		}
	} else {
		if err := store.AddPostToTopic(topic.ID, msg, userName, ipAddr); err != nil {
			logger.Errorf("createNewPost(): store.AddPostToTopic() failed with %s", err)
		}
		http.Redirect(w, r, fmt.Sprintf("/%s?tid=%d", model.ForumUrl, topic.ID), 302)
	}
}

// url: /{forum}/newpost[?topicId={topicId}]
func handleNewPost(forum *Forum, w http.ResponseWriter, r *http.Request) {
	var topic *Topic
	topicID := 0

	if topicIDStr := strings.TrimSpace(r.FormValue("tid")); topicIDStr != "" {
		topicID, _ = strconv.Atoi(topicIDStr)
		if topic = forum.Store.TopicByID(uint32(topicID)); topic == nil {
			logger.Noticef("handleNewPost(): invalid topicId: %d\n", topicID)
			http.Redirect(w, r, fmt.Sprintf("/%s", forum.ForumUrl), 302)
			return
		}
	}

	//fmt.Printf("handleNewPost(): forum: %q, topicId: %d\n", forum.ForumUrl, topicId)
	cookie := getSecureCookie(r)
	model := &ModelNewPost{
		Forum:    *forum,
		TopicID:  topicID,
		PrevName: cookie,
		Token:    session.NewString(IPAddress(ipAddrToInternal(getIPAddress(r)))),
	}

	if topic != nil {
		model.TopicLocked = topic.Locked
	}

	if r.Method == "POST" {
		createNewPost(w, r, model, topic)
		return
	}

	if topicID != 0 {
		model.PrevSubject = topic.Subject
	}

	ExecTemplate(w, tmplNewPost, model)
}
