package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gojp/kana"
	"golang.org/x/net/html"
)

const (
	defaultURL         = "https://www.yomiuri.co.jp"
	defaultSearchDepth = 1
	maxSearchDepth     = 10
	defaultRankingSize = 100
)

type scraperOptions struct {
	searchDepth *int
	loggingMode bool
}

type Option func(*scraperOptions) error

// kanjiKanaFrequencyCounter describes the counting of Kanji, kanas e hiraganas.
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
}

func main() {

	var (
		url         string
		searchDepth int
		rankingSize int
	)

	flag.StringVar(&url, "url", defaultURL, "target website")
	flag.IntVar(&searchDepth, "depth", defaultSearchDepth, "search depth")
	flag.IntVar(&rankingSize, "ranksize", defaultRankingSize, "ranking size")
	flag.Parse()

	startExecTime := time.Now()
	res, err := newKanjiKanaScraper(url, WithSearchDepth(searchDepth), WithLogging())
	if err != nil {
		log.Println(err)
	}

	mostCommonKanjis := getMostCommonCharactersList(res.kanjis)
	mostCommonKatakana := getMostCommonCharactersList(res.katakanas)
	mostCommonHiragana := getMostCommonCharactersList(res.hiraganas)

	fmt.Println("All Japanese characters found:", res.allCharacteresCount)
	fmt.Println("Kanji unique count:", res.kanjiUniqueCount)

	kanjiRankingSize := min(res.kanjiUniqueCount, rankingSize)
	if res.kanjiUniqueCount > 0 {
		fmt.Println(kanjiRankingSize, "most common Kanji characters:")
		printCharactersRanking(res.kanjis, mostCommonKanjis, kanjiRankingSize)
	}

	fmt.Println("Kana unique count:", res.kanaUniqueCount)
	fmt.Println("Katakana unique count:", res.katakanaUniqueCount)
	fmt.Println("Hiragana unique count:", res.hiraganaUniqueCount)

	katakanaRankingSize := min(res.katakanaUniqueCount, rankingSize)
	if res.katakanaUniqueCount > 0 {
		fmt.Println(katakanaRankingSize, "most common Katakana characters:")
		printCharactersRanking(res.katakanas, mostCommonKatakana, katakanaRankingSize)
	}

	hiraganaRankingSize := min(res.hiraganaUniqueCount, rankingSize)
	if res.hiraganaUniqueCount > 0 {
		fmt.Println(hiraganaRankingSize, "most common Hiragana characters:")
		printCharactersRanking(res.hiraganas, mostCommonHiragana, hiraganaRankingSize)
	}

	log.Printf("total time: %v ms\n", time.Since(startExecTime))
}

func printCharactersRanking(m map[string]int, rankingList []string, rankingSize int) {
	minRankingSize := rankingSize
	if len(rankingList) < minRankingSize {
		minRankingSize = len(rankingList)
	}
	for i := 0; i < minRankingSize; i++ {
		if kana.IsKana(rankingList[i]) {
			romaji := kana.KanaToRomaji(rankingList[i])
			fmt.Printf("%4d. %v %v (%v)\n", i+1, rankingList[i], romaji, m[rankingList[i]])
		} else {
			fmt.Printf("%4d. %v (%v)\n", i+1, rankingList[i], m[rankingList[i]])
		}
	}
	fmt.Println()
}

func getMostCommonCharactersList(m map[string]int) []string {
	var i int
	charactersList := make([]string, len(m))
	for k := range m {
		charactersList[i] = k
		i += 1
	}

	sort.SliceStable(charactersList, func(i, j int) bool {
		return m[charactersList[i]] > m[charactersList[j]]
	})

	return charactersList
}

func (counter *kanjiKanaFrequencyCounter) routine(ctx context.Context, url string, layer int) {
	if layer < 0 {
		return
	}

	resp, err := http.Get(url)
	if err != nil {
		fmt.Println("unable to fetch url", err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("fail to read response body", err)
		return
	}
	text := string(body)
	for _, r := range text {
		c := string(r)
		if kana.IsKanji(c) || kana.IsKatakana(c) || kana.IsHiragana(c) {
			counter.allCharacteresCount += 1
			if kana.IsKanji(c) {
				counter.kanjis[c] += 1
			}
			if kana.IsKatakana(c) {
				counter.katakanas[c] += 1
			}
			if kana.IsHiragana(c) {
				counter.hiraganas[c] += 1
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
		counter.routine(ctx, nextURL, layer-1)
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

	if !validateURL(rootURL) {
		rootURL = defaultURL
		if opts.loggingMode {
			log.Printf("invalid URL: setting to default URL: %s\n", rootURL)
		}
	}

	var searchDepth int
	if opts.searchDepth == nil {
		searchDepth = defaultSearchDepth
	} else {
		searchDepth = *opts.searchDepth
	}

	if opts.loggingMode {
		log.Printf("search depth set to %v\n", searchDepth)
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
	for c := range frequencyCounter.katakanas {
		kanas[c] = struct{}{}
	}

	for c := range frequencyCounter.hiraganas {
		kanas[c] = struct{}{}
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

func WithLogging() Option {
	return func(opts *scraperOptions) error {
		opts.loggingMode = true
		log.Println("logging mode is set")
		return nil
	}
}

func validateURL(url string) bool {
	// TODO weak test that needs to be improved
	return strings.HasPrefix(url, "http") && strings.Count(url, "://www.") == 1
}
