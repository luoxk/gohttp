package gohttp

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/tls"
	"encoding/hex"
	. "fmt"
	"golang.org/x/net/http2"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type interceptor func(r *BasicRequest)

type Transport struct {
	upstream http.RoundTripper
	cookies  http.CookieJar
}

const userAgent = `Mozilla/5.0 (Windows NT 6.1) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/41.0.2228.0 Safari/537.36`

func (t *Transport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Header.Get("User-Agent") == "" {
		r.Header.Set("User-Agent", userAgent)
	}
	if t.cookies != nil {
		for _, cookie := range t.cookies.Cookies(r.URL) {
			r.AddCookie(cookie)
		}
	}

	resp, err := t.upstream.RoundTrip(r)
	if err != nil {
		return nil, err
	}

	if t.cookies != nil {
		if rc := resp.Cookies(); len(rc) > 0 {
			t.cookies.SetCookies(r.URL, rc)
		}
	}

	return resp, err
}

var (
	ProxyClientPool    *sync.Pool
	My_ProxyClientPool *sync.Pool
	ClientPool         *sync.Pool
)

func NewTransport(upstream http.RoundTripper) (*Transport, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	return &Transport{upstream: upstream, cookies: jar}, nil
}

type BasicHttpClient struct {
	client       *http.Client
	interceptors []interceptor
	lock         sync.Mutex
	queue        []*BasicRequest
	tempQueue    []*BasicRequest
	getQueueNum  chan chan int
	pushQueue    chan *BasicRequest
	releaseChan  chan bool
	doingNum     int64
	tick         chan bool
	Pipe         *SuperPipe
	ctx          context.Context
}

func (b *BasicHttpClient) Hello() {
	println("hello")
}

/*
func NewProxyProHttpClient(proxyUser, proxyPass string) *BasicHttpClient {
	proxyU, _ := url.Parse(fmt.Sprintf("http://%s:%s@http-pro.abuyun.com:9010", proxyUser, proxyPass))
	return &BasicHttpClient{
		client: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig:   &tls.Config{InsecureSkipVerify: true},
				DisableKeepAlives: true,
				IdleConnTimeout:   10 * time.Second,

				Proxy: http.ProxyURL(proxyU),
			},
			Timeout: time.Second * 60 * 5,
			Jar:     &Jar{},
		},
	}
}*/

func (b *BasicHttpClient) StartQueue(stopChan <-chan bool, suffix string) {
	var tas = make(chan *BasicRequest, 1e5)
	/*for i := 0; i < 50; i++ {
		go func() {
			for {
				select {
				case r := <-tas:
					time.AfterFunc(time.Second, func() {
						b.DoRequestReadLimit(r, func() {})
					})
				}
			}
		}()
	}*/

	go func() {
		for {
			select {
			case req := <-b.pushQueue:
				b.queue = append(b.queue, req)
			case num := <-b.getQueueNum:
				num <- len(b.queue)
			case <-stopChan:
				return
			default:
				if len(b.queue) > 0 {

					req := b.queue[0]
					switch suffix {
					default:
						b.queue = b.queue[1:]
						if atomic.LoadInt64(&b.doingNum) < 80 {
							tas <- req
						} else {
							b.queue = append(b.queue, req)
						}

					}
				}
			}
		}
	}()
}

func (b *BasicHttpClient) GetQueueLen() int {
	length := make(chan int, 1)
	b.getQueueNum <- length
	return <-length
}

func (b *BasicHttpClient) Push(req *BasicRequest) {
	b.pushQueue <- req
}

func (b *BasicHttpClient) Info() string {
	return Sprintf("requesting Num [%d/100]", atomic.LoadInt64(&b.doingNum))
}

func (b *BasicHttpClient) Num() int64 {
	return atomic.LoadInt64(&b.doingNum)
}

func (hc *BasicHttpClient) DoRequestReadLimit(b *BasicRequest, cb func(res int)) {
	atomic.AddInt64(&hc.doingNum, 1)
	defer func() {
		atomic.AddInt64(&hc.doingNum, -1)
	}()

	for _, callback := range hc.interceptors {
		callback(b)
	}
	req, err := http.NewRequest(b.Method, b.UrlString(), bytes.NewReader(b.BodyToBinary()))
	if err != nil {
		log.Println(err.Error())
		return
	}

	b.ForEachHeaders(func(k, v string) {
		req.Header[k] = append(req.Header[k], v)
	})

	//req.Header["Cookie"] = append(req.Header["Cookie"], b.CookieToStr())

	resp, err := hc.client.Do(req)
	defer func() {
		if resp != nil {
			resp.Body.Close()
		}
	}()
	if err != nil {
		log.Println("gohttp ---> ", err.Error())
		return
	}
	resp.Body.Read(make([]byte, 1024))
	cb(resp.StatusCode)
}

func newproxyClient(proxyUser, proxyPass string, ash2 bool) *BasicHttpClient {
	proxyU, _ := url.Parse("http://" + proxyUser + ":" + proxyPass + "@http-dyn.abuyun.com:9020")
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
			//RootCAs:caCertPool,
		},
		Proxy: http.ProxyURL(proxyU),
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 5 * time.Second,
			DualStack: true,
		}).DialContext,
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   100 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	scraper, err := NewTransport(tr)
	if err != nil {
		log.Println(err)
		return nil
	}

	b := &BasicHttpClient{
		client: &http.Client{
			Transport: scraper,
			Jar:       scraper.cookies,
		},

		queue:       make([]*BasicRequest, 0),
		getQueueNum: make(chan chan int, 1e4),
		pushQueue:   make(chan *BasicRequest, 1e4),
		releaseChan: make(chan bool, 1e2),
	}

	if ash2 {
		//px,_ := proxy.SOCKS5("tcp4","127.0.0.1:1080",nil,proxy.Direct)
		//tr.Dial = px.Dial
		if err := http2.ConfigureTransport(tr); err != nil {
			log.Println(err)
		}
	}
	b.Pipe = NewSuperPipe(b)
	return b
}

func NewProxyHttpClient(proxyUser, proxyPass string, ash2 bool) *BasicHttpClient {

	if ProxyClientPool == nil {
		ProxyClientPool = &sync.Pool{
			New: func() interface{} {
				return newproxyClient(proxyUser, proxyPass, ash2)
			},
		}

		for i := 0; i < 32; i++ {
			ProxyClientPool.Put(newproxyClient(proxyUser, proxyPass, ash2))
		}
	}

	return ProxyClientPool.Get().(*BasicHttpClient)
	return newproxyClient(proxyUser, proxyPass, ash2)
}

func NewMyProxyClient(appkey, secret string, ash2 bool) *BasicHttpClient {
	if My_ProxyClientPool == nil {
		My_ProxyClientPool = &sync.Pool{
			New: func() interface{} {
				return newMyClient(appkey, secret, ash2)
			},
		}

		for i := 0; i < 32; i++ {
			My_ProxyClientPool.Put(newMyClient(appkey, secret, ash2))
		}
	}
	return My_ProxyClientPool.Get().(*BasicHttpClient)
}

func newMyClient(appkey, secret string, ash2 bool) *BasicHttpClient {
	proxyU, _ := url.Parse(("http://s5.proxy.mayidaili.com:8123"))
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
			//RootCAs:caCertPool,
		},
		Proxy: http.ProxyURL(proxyU),
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 5 * time.Second,
			DualStack: true,
		}).DialContext,
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   100 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	scraper, err := NewTransport(tr)
	if err != nil {
		log.Println(err)
		return nil
	}

	b := &BasicHttpClient{
		client: &http.Client{
			Transport: scraper,
			Jar:       scraper.cookies,
		},

		queue:       make([]*BasicRequest, 0),
		getQueueNum: make(chan chan int, 1e4),
		pushQueue:   make(chan *BasicRequest, 1e4),
		releaseChan: make(chan bool, 1e2),
	}

	if ash2 {
		//px,_ := proxy.SOCKS5("tcp4","127.0.0.1:1080",nil,proxy.Direct)
		//tr.Dial = px.Dial
		if err := http2.ConfigureTransport(tr); err != nil {
			log.Println(err)
		}
	}
	b.AddInterceptor(func(r *BasicRequest) {
		r.AuthHeader(appkey, secret)
	})
	return b
}

func NewXdailiClient() *BasicHttpClient {
	proxyU, _ := url.Parse(("http://forward.xdaili.cn:80"))
	scraper, err := NewTransport(&http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
		Proxy: http.ProxyURL(proxyU),
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
			DualStack: true,
		}).DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   100 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	})
	if err != nil {
		log.Println(err)
		return nil
	}
	return &BasicHttpClient{
		client: &http.Client{
			Transport: scraper,
			Jar:       scraper.cookies,
		},
	}
}

func newHttpClient(ctx context.Context, ash2 bool) *BasicHttpClient {
	//proxyU, _ := url.Parse(("http://127.0.0.1:8888"))

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
		//Proxy:                 http.ProxyURL(proxyU),
		MaxIdleConns:          100,
		IdleConnTimeout:       50 * time.Second,
		TLSHandshakeTimeout:   50 * time.Second,
		ExpectContinueTimeout: 10 * time.Second,
	}

	scraper, err := NewTransport(tr)
	if err != nil {
		log.Println(err)
		return nil
	}

	if ash2 {
		//px,_ := proxy.SOCKS5("tcp4","127.0.0.1:1080",nil,proxy.Direct)
		//tr.Dial = px.Dial
		if err := http2.ConfigureTransport(tr); err != nil {
			panic(err)
		}
	}

	b := &BasicHttpClient{
		client: &http.Client{
			Transport: scraper,
			Jar:       scraper.cookies,
		},
	}
	if ctx != nil {
		b.ctx = ctx
	}
	return b
}
func NewHttpClientWithProxy(proxy string) *BasicHttpClient {

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
		//Proxy:                 http.ProxyURL(proxyU),
		MaxIdleConns:          100,
		IdleConnTimeout:       50 * time.Second,
		TLSHandshakeTimeout:   50 * time.Second,
		ExpectContinueTimeout: 10 * time.Second,
	}
	var proxyU *url.URL
	if !strings.HasPrefix(proxy, "http://") {
		proxy = "http://" + proxy
	}
	proxyU, _ = url.Parse(proxy)
	tr.Proxy = http.ProxyURL(proxyU)

	scraper, err := NewTransport(tr)

	if err != nil {
		log.Println(err)
		return nil
	}

	b := &BasicHttpClient{
		client: &http.Client{
			Transport: scraper,
			Jar:       scraper.cookies,
		},
	}
	return b
}

func NewHttpClientWithConfig(ctx context.Context, config struct {
	Ash2     bool
	ProxyUrl string
	TimeOut  time.Duration
}) *BasicHttpClient {

	//proxyU, _ := url.Parse(("http://127.0.0.1:8888"))

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
		//Proxy:                 http.ProxyURL(proxyU),
		MaxIdleConns:          100,
		IdleConnTimeout:       50 * time.Second,
		TLSHandshakeTimeout:   50 * time.Second,
		ExpectContinueTimeout: 10 * time.Second,
	}
	if config.ProxyUrl != "" && len(config.ProxyUrl) > 0 {
		var proxyU *url.URL
		if !strings.HasPrefix(config.ProxyUrl, "http://") {
			config.ProxyUrl = "http://" + config.ProxyUrl
		}
		proxyU, _ = url.Parse(config.ProxyUrl)
		tr.Proxy = http.ProxyURL(proxyU)
	}

	if config.TimeOut >= time.Second {
		ctx, _ = context.WithTimeout(ctx, config.TimeOut)
	}

	scraper, err := NewTransport(tr)

	if err != nil {
		log.Println(err)
		return nil
	}

	if config.Ash2 {
		//px,_ := proxy.SOCKS5("tcp4","127.0.0.1:1080",nil,proxy.Direct)
		//tr.Dial = px.Dial
		if err := http2.ConfigureTransport(tr); err != nil {
			panic(err)
		}
	}

	b := &BasicHttpClient{
		client: &http.Client{
			Transport: scraper,
			Jar:       scraper.cookies,
		},
	}
	if ctx != nil {
		b.ctx = ctx
	}
	return b
}

func NewHttpClientWithContext(ctx context.Context, as_http_2 bool) *BasicHttpClient {
	return newHttpClient(ctx, as_http_2)
}
func NewHttpClient(as_http_2 bool) *BasicHttpClient {
	if ClientPool == nil {
		ClientPool = &sync.Pool{
			New: func() interface{} {
				return newHttpClient(nil, as_http_2)
			},
		}
		for i := 0; i < 32; i++ {
			ClientPool.Put(newHttpClient(nil, as_http_2))
		}
	}

	return ClientPool.Get().(*BasicHttpClient)
	return newHttpClient(nil, as_http_2)
}

type Response struct {
	Request    *BasicRequest
	Cookies    []*http.Cookie
	Url        string
	Context    string
	RawBody    []byte
	StatusCode int
	Headers    map[string][]string
	Proto      string
}

func (this *Response) ContextContain(s string) bool {
	return strings.Contains(this.Context, s)
}

func (this *Response) HeadersContain(key, s string) bool {
	if value, ok := this.Headers[key]; ok {
		return strings.Contains(value[0], s)
	}
	return false
}

func (hc *BasicHttpClient) DoRequest2(b *BasicRequest) (response *http.Response, err error) {
	req, err := http.NewRequest(b.Method, b.UrlString(), bytes.NewReader(b.BodyToBinary()))
	if hc.ctx != nil {
		req = req.WithContext(hc.ctx)
		b.Ctx = req.Context()
	}
	for _, callback := range hc.interceptors {
		callback(b)
	}

	if err != nil {
		log.Println(err.Error())
		return
	}

	b.ForEachHeaders(func(k, v string) {
		req.Header[k] = append(req.Header[k], v)
	})
	resp, err := hc.client.Transport.RoundTrip(req)
	return resp, err

}

func (hc *BasicHttpClient) DoRequest(b *BasicRequest) (response *Response, err error) {
	req, err := http.NewRequest(b.Method, b.UrlString(), bytes.NewReader(b.BodyToBinary()))
	if hc.ctx != nil {
		req = req.WithContext(hc.ctx)
		b.Ctx = req.Context()
	}
	for _, callback := range hc.interceptors {
		callback(b)
	}

	if err != nil {
		//log.Println(err.Error())
		return
	}

	b.ForEachHeaders(func(k, v string) {
		req.Header[k] = append(req.Header[k], v)
	})

	resp, err := hc.client.Transport.RoundTrip(req)
	defer func() {
		if resp != nil {
			resp.Body.Close()
		}
	}()
	if err != nil {
		//log.Println("gohttp ---> ", err.Error())

		return
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		//log.Println("gohttp ---> ", err.Error())
		return
	}
	for _, cookie := range resp.Cookies() {
		b.AddCookie(cookie.Name, cookie.Value)
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	response = &Response{
		Request:    b,
		Url:        b.UrlString(),
		Cookies:    resp.Cookies(),
		Context:    string(data),
		RawBody:    data,
		StatusCode: resp.StatusCode,
		Headers:    resp.Header,
		Proto:      resp.Proto,
	}

	return
}

func (hc *BasicHttpClient) AsyncDoRequest(b *BasicRequest, callback func(response *Response, err error)) {
	callback(hc.DoRequest(b))
}

func (hc *BasicHttpClient) Context() context.Context {
	return hc.ctx
}

func MD5Hash(code string) string {
	m := md5.New()
	m.Write([]byte(code))
	return hex.EncodeToString(m.Sum(nil))[8:24]
}

func (this *BasicHttpClient) Get(url, ua string) (s string) {

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Println(err)
		return
	}
	req.Header.Set("User-Agent", ua)

	resp, err := this.client.Do(req)
	if err != nil {
		log.Println(err)
		return
	}
	defer func() {
		if resp != nil {
			resp.Body.Close()
		}
	}()

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Println(err.Error())
		return
	}
	s = string(data)
	return
}

func (this *BasicHttpClient) AsyncGet(url, ua string, callback func(s string)) {
	callback(this.Get(url, ua))
}

func (this *BasicHttpClient) AddInterceptor(f func(r *BasicRequest)) {
	this.interceptors = append(this.interceptors, f)
}
