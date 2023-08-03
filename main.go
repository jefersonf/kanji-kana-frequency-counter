package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gojp/kana"
	"golang.org/x/net/html"
)

const (
	defaultURL         = "https://www.jst.go.jp"
	defaultSearchDepth = 1
	maxSearchDepth     = 3
)

type scraperOptions struct {
	searchDepth *int
	loggingMode bool
}

type Option func(*scraperOptions) error

type kanjiKanaFrequencyCounter struct {
	allCharacteresCount int
	uniqueCount         int
	kanjiUniqueCount    int
	kanaUniqueCount     int
	hiraganaUniqueCount int
	katakanaUniqueCount int
	kanjis              map[string]int
	hiraganas           map[string]int
	katakanas           map[string]int
	lock                *sync.Mutex
}

func main() {

	// "https://kanjikana.com/en/kanji/jlpt/n5"

	var (
		url         string
		searchDepth int
	)

	flag.StringVar(&url, "url", defaultURL, "target website")
	flag.IntVar(&searchDepth, "depth", defaultSearchDepth, "search depth")
	flag.Parse()

	res, err := newKanjiKanaScraper(url, WithSearchDepth(searchDepth), WithLogging())
	if err != nil {
		log.Println(err)
	}
	fmt.Println(res)
}

func (fc *kanjiKanaFrequencyCounter) routine(ctx context.Context, url string, layer int) {
	if layer < 0 {
		return
	}

	resp, err := http.Get(url)
	if err != nil {
		fmt.Println("unable to fetching url. Err:", err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("unable to read body. Err:", err)
		return
	}
	text := string(body)
	for _, r := range text {
		c := string(r)
		if kana.IsKanji(c) || kana.IsKatakana(c) || kana.IsHiragana(c) {
			fc.allCharacteresCount += 1

			if kana.IsKanji(c) {
				fc.kanjis[c] += 1
			}

			if kana.IsKatakana(c) {
				fc.katakanas[c] += 1
			}

			if kana.IsHiragana(c) {
				fc.hiraganas[c] += 1
			}
		}
	}

	links := make(map[string]struct{})
	reader := strings.NewReader(text)
	tokenizer := html.NewTokenizer(reader)

	for {
		tokenType := tokenizer.Next()
		if tokenType == html.ErrorToken {
			break
		}

		token := tokenizer.Token()
		if tokenType == html.StartTagToken && token.Data == "a" {
			for _, attr := range token.Attr {
				if attr.Key == "href" {
					check := len(attr.Val) > 0 && !strings.HasPrefix(attr.Val, "http")
					check = check && !strings.HasPrefix(attr.Val, "#")
					check = check && !strings.HasPrefix(attr.Val, "..")
					check = check && strings.HasSuffix(attr.Val, ".html")
					if check {
						links[url+"/"+attr.Val] = struct{}{}
					}
				}
			}
		}
	}

	for nextURL := range links {
		fc.routine(ctx, nextURL, layer-1)
	}
}

func newKanjiKanaScraper(rootURL string, options ...Option) (*kanjiKanaFrequencyCounter, error) {

	var opts scraperOptions
	for _, opt := range options {
		err := opt(&opts)
		if err != nil {
			return nil, err
		}
	}

	if !validURL(rootURL) {
		rootURL = defaultURL
		if opts.loggingMode {
			log.Println("invalid URL, setting to default URL=", rootURL)
		}
	}

	var searchDepth int
	if opts.searchDepth == nil {
		searchDepth = defaultSearchDepth
	} else {
		searchDepth = *opts.searchDepth
	}

	if opts.loggingMode {
		log.Println("search depth set to", searchDepth)
	}

	frequencyCounter := &kanjiKanaFrequencyCounter{
		kanjis:    make(map[string]int),
		katakanas: make(map[string]int),
		hiraganas: make(map[string]int),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	frequencyCounter.routine(ctx, rootURL, *opts.searchDepth)

	<-ctx.Done()

	frequencyCounter.uniqueCount += len(frequencyCounter.kanjis)
	frequencyCounter.uniqueCount += len(frequencyCounter.katakanas)
	frequencyCounter.uniqueCount += len(frequencyCounter.hiraganas)

	frequencyCounter.kanjiUniqueCount = len(frequencyCounter.kanjis)
	frequencyCounter.katakanaUniqueCount = len(frequencyCounter.katakanas)
	frequencyCounter.hiraganaUniqueCount = len(frequencyCounter.hiraganas)

	kanas := make(map[string]struct{})
	for k := range frequencyCounter.katakanas {
		kanas[k] = struct{}{}
	}

	for k := range frequencyCounter.hiraganas {
		kanas[k] = struct{}{}
	}

	frequencyCounter.kanaUniqueCount = len(kanas)

	return frequencyCounter, nil
}

func WithSearchDepth(depth int) Option {
	return func(opts *scraperOptions) error {
		if depth < 0 {
			return errors.New("search depth should be positive")
		}
		if depth >= maxSearchDepth {
			return errors.New("search depth exceeds default maximum depth")
		}
		opts.searchDepth = &depth
		return nil
	}
}

// func WithContext() Option {
// 	return func(opts *scraperOptions) error {
// 		opts.loggingMode = true
// 		log.Println("logging mode is set")
// 		return nil
// 	}
// }

func WithLogging() Option {
	return func(opts *scraperOptions) error {
		opts.loggingMode = true
		log.Println("logging mode is set")
		return nil
	}
}

func validURL(url string) bool {
	return true
}
