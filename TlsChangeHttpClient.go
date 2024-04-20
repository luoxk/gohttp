package gohttp

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"fmt"
	"github.com/Carcraftz/cclient"
	"github.com/Carcraftz/fhttp"
	"github.com/Carcraftz/fhttp/cookiejar"
	tls "github.com/Carcraftz/utls"
	"github.com/andybalholm/brotli"
	"io/ioutil"
	"net/url"

	"log"
)

type GohttpClient interface {
	DoRequest(b *BasicRequest) (response *Response, err error)
	AddInterceptor(f func(r *BasicRequest))
	SetCookies(u *url.URL, cookies []*http.Cookie)
	GetCookie(url *url.URL, name string) *http.Cookie
	GetCookies(url *url.URL, name string) []*http.Cookie
}

type TLsChangeHttpClient struct {
	client       http.Client
	interceptors []interceptor
}

func (this *TLsChangeHttpClient) GetCookies(url *url.URL, name string) []*http.Cookie {
	cks := this.client.Jar.Cookies(url)

	return cks
}

func (this *TLsChangeHttpClient) GetCookie(url *url.URL, name string) *http.Cookie {
	cks := this.client.Jar.Cookies(url)
	for _, ck := range cks {
		if ck.Name == name {
			return ck
		}
	}
	return nil
}

func (this *TLsChangeHttpClient) AddInterceptor(f func(r *BasicRequest)) {
	this.interceptors = append(this.interceptors, f)
}

func NewTlsHttpClient(proxyAddr, customId string) *TLsChangeHttpClient {
	client, err := cclient.NewClient(tls.HelloIOS_Auto, proxyAddr, true, 1000)
	if err != nil {
		log.Println(err)
		return nil
	}
	jar, err := cookiejar.New(nil)

	if err != nil {
		log.Println(err)
		return nil
	}
	client.Jar = jar

	tch := &TLsChangeHttpClient{
		client: client,
	}
	return tch
}

func (hc *TLsChangeHttpClient) SetCookies(u *url.URL, cookies []*http.Cookie) {
	hc.client.Jar.SetCookies(u, cookies)
}

func (hc *TLsChangeHttpClient) DoRequest(b *BasicRequest) (response *Response, err error) {
	if !b.AllowRedirect {
		hc.client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	} else {
		hc.client.CheckRedirect = nil
	}
	req, err := http.NewRequest(b.Method, b.UrlString(), bytes.NewReader(b.BodyToBinary()))
	if err != nil {
		return
	}
	for _, callback := range hc.interceptors {
		callback(b)
	}

	req.Header = http.Header{
		http.PHeaderOrderKey: {":method", ":authority", ":scheme", ":path"},
	}
	b.ForEachHeaders(func(k, v string) {
		req.Header[k] = append(req.Header[k], v)
	})
	uri, _ := url.Parse(b.UrlString())
	for _, cookie := range hc.client.Jar.Cookies(uri) {
		b.AddCookie(cookie.Name, cookie.Value)
	}

	resp, err := hc.client.Do(req)
	defer func() {
		if resp != nil {
			resp.Body.Close()
		}
	}()
	if err != nil {
		log.Println(err)
		return nil, err
	}
	encoding := resp.Header["Content-Encoding"]
	body, err := ioutil.ReadAll(resp.Body)
	finalres := ""
	if err != nil {
		return
	}
	if len(encoding) > 0 {
		if encoding[0] == "gzip" {
			unz, err := gUnzipData(body)
			if err != nil {
				return nil, err
			}
			finalres = string(unz)
		} else if encoding[0] == "deflate" {
			unz, err := enflateData(body)
			if err != nil {
				return nil, err
			}
			finalres = string(unz)
		} else if encoding[0] == "br" {
			unz, err := unBrotliData(body)
			if err != nil {
				return nil, err
			}
			finalres = string(unz)
		} else {
			fmt.Println("UNKNOWN ENCODING: " + encoding[0])
			finalres = string(body)
		}
	} else {
		finalres = string(body)
	}
	response = &Response{
		Request: b,
		Url:     b.UrlString(),

		Context:    string(finalres),
		StatusCode: resp.StatusCode,
		Headers:    resp.Header,
		Proto:      resp.Proto,
	}
	return
}

func gUnzipData(data []byte) (resData []byte, err error) {
	gz, _ := gzip.NewReader(bytes.NewReader(data))
	defer gz.Close()
	respBody, err := ioutil.ReadAll(gz)
	return respBody, err
}
func enflateData(data []byte) (resData []byte, err error) {
	zr, _ := zlib.NewReader(bytes.NewReader(data))
	defer zr.Close()
	enflated, err := ioutil.ReadAll(zr)
	return enflated, err
}
func unBrotliData(data []byte) (resData []byte, err error) {
	br := brotli.NewReader(bytes.NewReader(data))
	respBody, err := ioutil.ReadAll(br)
	return respBody, err
}
