package gohttp

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

type BasicRequest struct {
	Scheme        string
	Host          string
	path          string
	Method        string
	Query         [][]string
	FormQuery     [][]string
	Headers       [][]string
	UserInfo      [][]string
	Cookies       map[string]string
	FormData      []byte
	Ctx           context.Context
	AllowRedirect bool
}

func EmptyBasicRequest(ssl bool) *BasicRequest {
	r := &BasicRequest{}
	if ssl {
		r.Scheme = "https://"
	} else {
		r.Scheme = "http://"
	}
	return r
}

func NewRequest(method, host, path string, ssl bool) *BasicRequest {
	r := &BasicRequest{Method: method, Host: host, path: path, Cookies: map[string]string{}}
	if ssl {
		r.Scheme = "https://"
	} else {
		r.Scheme = "http://"
	}
	//r.AddHeaders("Upgrade-Insecure-Requests", "1")
	//r.AddHeaders("User-Agent", "Mozilla/5.0 (Windows NT 6.1; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/59.0.3071.86 Safari/537.36")
	//r.AddHeaders("Accept-Language", "en-US,en;q=0.9")
	//r.AddHeaders("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,image/apng,*/*;q=0.8")
	return r
}

func NewRequestWithCtx(ctx context.Context, method, host, path string, ssl bool) *BasicRequest {
	r := NewRequest(method, host, path, ssl)
	r.Ctx = ctx
	return r
}

func (r *BasicRequest) Context() context.Context {
	return r.Ctx
}

func (r *BasicRequest) Path() string {
	u, _ := url.Parse(r.UrlString())
	return u.EscapedPath()
}

func NewBasicRequest(method, host, path string) *BasicRequest {
	sch := ""
	if strings.HasPrefix(host, "http://") {
		sch = "http://"
		host = strings.Replace(host, "http://", "", -1)
	} else if strings.HasPrefix(host, "https://") {
		sch = "https://"
		host = strings.Replace(host, "https://", "", -1)
	}
	r := &BasicRequest{Method: method, Host: host, path: path, Scheme: sch, Cookies: map[string]string{}}
	r.AddHeaders("Upgrade-Insecure-Requests", "1")

	//Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/74.0.3729.169 Safari/537.36
	//Mozilla/5.0 (Windows NT 6.1; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/59.0.3071.86 Safari/537.36
	r.AddHeaders("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/74.0.3729.169 Safari/537.36")
	r.AddHeaders("Accept-Language", "en-US,en;q=0.9")
	r.AddHeaders("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,image/apng,*/*;q=0.8")
	r.AddHeaders("Content-Type", "application/x-www-form-urlencoded")
	return r
}

func (r *BasicRequest) AddUserInfo(key, value string) {
	r.UserInfo = append(r.UserInfo, []string{key, value})
}

func (r *BasicRequest) AuthHeader(appKey, secret string) {
	paramMap := make(map[string]string)
	paramMap["app_key"] = appKey
	//time.Now().Format("2006-01-02 HH:mm:ss")
	timeStr := time.Now().Format("2006-01-02 15:04:05")
	paramMap["timestamp"] = timeStr
	keys := make([]string, 0)
	for k, _ := range paramMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	sb := make([]string, 0)
	sb = append(sb, secret)
	for _, v := range keys {
		sb = append(sb, v)
		sb = append(sb, paramMap[v])
	}
	sb = append(sb, secret)
	codes := strings.Join(sb, "")
	sign := MD5Hash(codes)
	paramMap["sign"] = sign
	header := fmt.Sprintf("MYH-AUTH-MD5 sign=%s&app_key=%s&timestamp=%s", sign, appKey, timeStr)
	r.AddHeaders("Proxy-Authorization", header)

}

func (r *BasicRequest) UserInfoToSince() []string {
	ret := []string{}
	for _, v := range r.UserInfo {
		for _, vv := range v {
			ret = append(ret, vv)
		}
	}
	return ret
}

func (r *BasicRequest) AddHeaders(key, value string) {
	flag := false
	if len(value) <= 0 || len(value) <= 0 {
		return
	}
	for k, v := range r.Headers {
		if v[0] == key {
			r.Headers[k][1] = value
			flag = true
		}
	}
	if !flag {
		r.Headers = append(r.Headers, []string{key, value})
	}

}

func (r *BasicRequest) GetHeaderByKey(key string) (res string) {
	for _, v := range r.Headers {
		if v[0] == key {
			return v[1]
		}
	}
	return res
}

func (r *BasicRequest) ForEachHeaders(f func(k, v string)) {
	for _, v := range r.Headers {
		f(v[0], v[1])
	}
}

func (r *BasicRequest) AddQuery(key, value string) {

	flag := false
	for k, v := range r.Query {
		if v[0] == key {
			r.Query[k][1] = value
			flag = true
		}
	}
	if !flag {
		r.Query = append(r.Query, []string{key, value})
	}
}

func (r *BasicRequest) QueryToStr() string {
	ret := ""
	for k, v := range r.Query {
		if k == 0 {
			ret += "?" + v[0] + "=" + url.QueryEscape(v[1])
		} else {
			ret += "&" + v[0] + "=" + url.QueryEscape(v[1])
		}
	}
	return ret
}

func (r *BasicRequest) AddCookie(key, value string) {
	r.Cookies[key] = value
}

func (r *BasicRequest) ContainCookie(key string) bool {
	return len(r.Cookies[key]) > 0
}

func (r *BasicRequest) CookieToStr() string {
	ret := ""
	for k, v := range r.Cookies {
		ret += k + "=" + v + ";"
	}
	return ret
}

func (r *BasicRequest) GetCookieByKey(name string) (res string) {
	return r.Cookies[name]
	return ""
}

func (r *BasicRequest) ParseCookies(raw string) {
	raw = strings.Replace(raw, " ", "", -1)
	keyAndvalue := strings.Split(raw, ";")
	for _, oneCookie := range keyAndvalue {
		kvArr := strings.Split(oneCookie, "=")
		if len(kvArr) >= 2 {
			r.AddCookie(kvArr[0], kvArr[1])
		}
	}
}

func (r *BasicRequest) GetFormQuery(key string) (value string) {
	for k, v := range r.FormQuery {
		if v[0] == key {
			value = r.FormQuery[k][1]
		}
	}
	return
}

func (r *BasicRequest) AddFormQuery(key, value string) {
	//fmt.Println(key,value)
	flag := false
	/*for k, v := range r.FormQuery {
		if v[0] == key {
			r.FormQuery[k][1] = value
			flag = true
		}
	}*/
	if !flag {
		r.FormQuery = append(r.FormQuery, []string{key, value})
	}

}

func (r *BasicRequest) FormQueryToStr() string {
	ret := ""
	for k, v := range r.FormQuery {
		if k == 0 {
			ret += v[0] + "=" + v[1]
		} else {
			ret += "&" + v[0] + "=" + v[1]
		}
	}
	return ret
}

func (r *BasicRequest) BodyToBinary() []byte {
	if r.FormData != nil {
		return r.FormData
	} else {
		ret := ""
		for k, v := range r.FormQuery {
			if k == 0 {
				ret += v[0] + "=" + url.QueryEscape(v[1])
			} else {
				ret += "&" + v[0] + "=" + url.QueryEscape(v[1])
			}
		}
		//ret = url.QueryEscape(ret)
		return []byte(ret)
	}
}

func (r *BasicRequest) UrlString() string {
	return r.Scheme + r.Host + r.path + r.QueryToStr()
}

func (r *BasicRequest) Origin() string {
	return r.Scheme + r.Host
}
