package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

// func main() {
// 	websiteURL := os.Getenv("WEBSITE_URL")
// 	assetsURL := os.Getenv("ASSETS_URL")

// 	pagesCrawled := make(map[string]bool)
// 	mediaMap := make(map[string][]string)

// 	var crawlFn func(pageURL string)
// 	crawlFn = func(pageURL string) {
// 		if pagesCrawled[pageURL] {
// 			return
// 		}
// 		pageHtml, err := getPageMedia(pageURL, assetsURL)
// 		if err != nil {
// 			log.Panicln(err)
// 		}
// 		mediaMatches := regexp.MustCompile(fmt.Sprintf(`["'](%s[^"' ]+)["']`, assetsURL)).FindAllStringSubmatch(string(pageHtml), -1)
// 		pageMedia := make([]string, len(mediaMatches))
// 		for i, match := range mediaMatches {
// 			m := match[1]
// 			m = strings.ReplaceAll(m, "&amp;", "&")
// 			m = strings.ReplaceAll(m, "\\u0026", "&")
// 			pageMedia[i] = m
// 		}
// 		log.Printf("Page %s: %d media files", websiteURL, len(pageMedia))
// 		pagesCrawled[pageURL] = true
// 		mediaMap[pageURL] = pageMedia

// 		subpageMatches := regexp.MustCompile(fmt.Sprintf(`["'](%s[^"' ]+)["']`, websiteURL)).FindAllStringSubmatch(string(pageHtml), -1)
// 		for _, subpageURL := range subpageMatches {
// 			crawlFn(subpageURL[1])
// 		}
// 	}
// 	crawlFn(websiteURL)

//		// log.Printf("Page %s: %d media files", websiteURL, len(pageMedia))
//		// for _, mediaURL := range pageMedia {
//		// 	startTime := time.Now()
//		// 	image, err := fetchMedia(mediaURL)
//		// 	if err != nil {
//		// 		log.Panicln(err)
//		// 	}
//		// 	log.Printf("  - Fetched %s (%.2fKB). Took %v", mediaURL, float64(len(image))/1000.0, time.Since(startTime))
//		// }
//	}
func main() {
	websiteURL := os.Getenv("WEBSITE_URL")
	assetsURL := os.Getenv("ASSETS_URL")

	pagesToCrawl, err := getPagesToCrawl(websiteURL)
	if err != nil {
		log.Panicln(err)
	}

	for _, pageURL := range pagesToCrawl {
		startTime := time.Now()
		// log.Printf("Crawling %s", pageURL)
		mediaMap := make(map[string][]string)
		pageMedia, err := getPageMedia(pageURL, assetsURL)
		if err != nil {
			log.Panicln(err)
		}
		mediaMap[pageURL] = pageMedia
		log.Printf("Page %s: %d media files (took %v)", pageURL, len(pageMedia), time.Since(startTime))
		for _, mediaURL := range pageMedia {
			startTime := time.Now()
			image, err := fetchMedia(mediaURL)
			if err != nil {
				log.Panicln(err)
			}
			log.Printf("  - Fetched %s (%.2fKB) (took %v)", mediaURL, float64(len(image))/1000.0, time.Since(startTime))
		}
	}
}

func getPagesToCrawl(websiteURL string) ([]string, error) {
	sitemapURL := websiteURL + "/sitemap.xml"
	resp, err := http.Get(sitemapURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s returned %d", sitemapURL, resp.StatusCode)
	}
	pageHtml, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	pageMatches := regexp.MustCompile(fmt.Sprintf(`>(%s[^<]+)<`, websiteURL)).FindAllStringSubmatch(string(pageHtml), -1)
	pages := make([]string, 0, len(pageMatches))
	for _, subpageURL := range pageMatches {
		pages = append(pages, subpageURL[1])
	}
	return pages, nil
}

func getPageMedia(pageURL string, assetsURL string) ([]string, error) {
	resp, err := http.Get(pageURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s returned %d", pageURL, resp.StatusCode)
	}
	pageHtml, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	mediaMatches := regexp.MustCompile(fmt.Sprintf(`["'](%s[^"' ]+)["']`, assetsURL)).FindAllStringSubmatch(string(pageHtml), -1)
	pageMedia := make([]string, len(mediaMatches))
	for i, match := range mediaMatches {
		m := match[1]
		m = strings.ReplaceAll(m, "&amp;", "&")
		m = strings.ReplaceAll(m, "\\u0026", "&")
		pageMedia[i] = m
	}
	return pageMedia, nil
}

func fetchMedia(mediaURL string) ([]byte, error) {
	resp, err := http.Get(mediaURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s returned %d", mediaURL, resp.StatusCode)
	}
	fmt.Println(resp.Header.Get("Content-Type"))
	return io.ReadAll(resp.Body)
}
