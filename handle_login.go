// This code is in Public Domain. Take all the code you want, I'll just write more.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type SecureCookieValue struct {
	AnonUser   string
	GithubUser string
	GithubName string
	GithubTemp string
}

func setSecureCookie(w http.ResponseWriter, cookieVal *SecureCookieValue) {
	val := make(map[string]string)
	val["anonuser"] = cookieVal.AnonUser
	val["ghuser"] = cookieVal.GithubUser
	val["ghname"] = cookieVal.GithubName
	val["ghtemp"] = cookieVal.GithubTemp
	if encoded, err := secureCookie.Encode(cookieName, val); err == nil {
		// TODO: set expiration (Expires    time.Time) long time in the future?
		cookie := &http.Cookie{
			Name:    cookieName,
			Value:   encoded,
			Path:    "/",
			Expires: time.Now().AddDate(1, 0, 0),
		}
		http.SetCookie(w, cookie)
	} else {
		fmt.Printf("setSecureCookie(): error encoding secure cookie %s\n", err)
	}
}

const WeekInSeconds = 60 * 60 * 24 * 7

// to delete the cookie value (e.g. for logging out), we need to set an
// invalid value
func deleteSecureCookie(w http.ResponseWriter) {
	cookie := &http.Cookie{
		Name:   cookieName,
		Value:  "deleted",
		MaxAge: WeekInSeconds,
		Path:   "/",
	}
	http.SetCookie(w, cookie)
}

func getSecureCookie(r *http.Request) *SecureCookieValue {
	ret := new(SecureCookieValue)
	if cookie, err := r.Cookie(cookieName); err == nil {
		// detect a deleted cookie
		if "deleted" == cookie.Value {
			return new(SecureCookieValue)
		}
		val := make(map[string]string)
		if err = secureCookie.Decode(cookieName, cookie.Value, &val); err != nil {
			// most likely expired cookie, so ignore. Ideally should delete the
			// cookie, but that requires access to http.ResponseWriter, so not
			// convenient for us
			logger.Noticef("Error decoding cookie %q, error: %s", cookie.Value, err)
			return new(SecureCookieValue)
		}
		var ok bool
		if ret.AnonUser, ok = val["anonuser"]; !ok {
			logger.Errorf("Error decoding cookie, no 'anonuser' field")
			return new(SecureCookieValue)
		}
		if ret.GithubUser, ok = val["ghuser"]; !ok {
			logger.Errorf("Error decoding cookie, no 'ghuser' field")
			return new(SecureCookieValue)
		}
		if ret.GithubTemp, ok = val["ghtemp"]; !ok {
			logger.Errorf("Error decoding cookie, no 'ghtemp' field")
			return new(SecureCookieValue)
		}
		if ret.GithubName, ok = val["ghname"]; !ok {
			logger.Errorf("Error decoding cookie, no 'ghname' field")
			return new(SecureCookieValue)
		}
	}
	return ret
}

func decodeUserFromCookie(r *http.Request) string {
	cookie := getSecureCookie(r)
	if cookie.GithubUser != "" {
		return cookie.GithubUser
	}
	return cookie.AnonUser
}

func decodeTwitterTempFromCookie(r *http.Request) string {
	return getSecureCookie(r).GithubUser
}

// url: GET /oauthtwittercb?redirect=$redirect
func handleOauthGithubCallback(w http.ResponseWriter, r *http.Request) {
	code := r.FormValue("code")
	if code == "" {
		httpErrorf(w, "Missing code value for callback")
		return
	}

	v := url.Values{
		"client_id":     {os.Getenv("CYVBACK_TOKEN")},
		"client_secret": {os.Getenv("CYVBACK_SECRET")},
		"code":          {code},
	}

	req, _ := http.NewRequest("POST", "https://github.com/login/oauth/access_token?"+v.Encode(), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		httpErrorf(w, err.Error())
		return
	}

	buf, _ := ioutil.ReadAll(resp.Body)
	resp.Body.Close()

	idx := bytes.Index(buf, []byte("access_token="))
	if idx == -1 {
		httpErrorf(w, "Invalid repsonse from Github")
		return
	}
	buf = buf[idx+13:]
	idx = bytes.Index(buf, []byte("&"))
	if idx == -1 {
		httpErrorf(w, "Invalid repsonse from Github")
		return
	}

	accessToken := string(buf[:idx])
	resp, err = http.DefaultClient.Get("https://api.github.com/user?access_token=" + accessToken)
	if err != nil {
		httpErrorf(w, err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		httpErrorf(w, "Invalid repsonse code from Github")
		return
	}

	buf, _ = ioutil.ReadAll(resp.Body)
	user := make(map[string]interface{})
	json.Unmarshal(buf, &user)

	if user["login"] == nil {
		httpErrorf(w, "Invalid user from Github")
		return
	}

	cookie := &SecureCookieValue{}
	cookie.GithubTemp = accessToken
	cookie.GithubUser, _ = user["login"].(string)
	cookie.GithubName, _ = user["name"].(string)
	setSecureCookie(w, cookie)

	http.Redirect(w, r, r.FormValue("redirect"), 301)
}

// url: GET /login?redirect=$redirect
func handleLogin(w http.ResponseWriter, r *http.Request) {
	redirect := strings.TrimSpace(r.FormValue("redirect"))
	if redirect == "" {
		httpErrorf(w, "Missing redirect value for /login")
		return
	}
	q := url.Values{
		"redirect": {redirect},
	}.Encode()

	cb := "http://" + r.Host + "/oauthgithubcb" + "?" + q

	q2 := url.Values{
		"redirect_uri": {cb},
		"client_id":    {os.Getenv("CYVBACK_TOKEN")},
	}.Encode()

	http.Redirect(w, r, "https://github.com/login/oauth/authorize?"+q2, 302)
}

// url: GET /logout?redirect=$redirect
func handleLogout(w http.ResponseWriter, r *http.Request) {
	redirect := strings.TrimSpace(r.FormValue("redirect"))
	if redirect == "" {
		httpErrorf(w, "Missing redirect value for /logout")
		return
	}
	deleteSecureCookie(w)
	http.Redirect(w, r, redirect, 302)
}
