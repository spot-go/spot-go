package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/gocolly/colly"
)

func extract(url string) {
	c := colly.NewCollector(colly.MaxBodySize(0))
	c.SetRequestTimeout(1 * time.Minute)

	c.OnHTML("tbody", func(e *colly.HTMLElement) {
		category := ""
		maxCodeSize := 0
		println("var emojis = map[string]Emoji{")

		e.ForEach("tr", func(i int, t *colly.HTMLElement) {
			v, _ := t.DOM.Children().First().Attr("class")
			if v == "mediumhead" {
				category = t.ChildText(".mediumhead")
			}
			if v == "rchars" {
				code := t.ChildText(".code")
				code = strings.ReplaceAll(code, "U+", "")
				if code != "" {
					name := t.ChildText(".name")
					fmt.Printf(
						"\t\"%s\": {\"%s\", \"%s\", \"%s\"},\n",
						code,
						category,
						code,
						name,
					)

					size := len(strings.Split(code, " "))
					if size > maxCodeSize {
						maxCodeSize = size
					}
				}
			}
		})
		println("}")
		println("var maxCodeSize = ", maxCodeSize)
	})

	c.OnError(func(r *colly.Response, err error) {
		panic(err)
	})

	c.Visit(url)
}

func main() {
	extract("https://unicode.org/emoji/charts/full-emoji-list.html")
	extract("https://unicode.org/emoji/charts/full-emoji-modifiers.html")
}
