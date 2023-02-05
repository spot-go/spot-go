package main

import (
	"time"

	"github.com/gocolly/colly"
)

func main() {
	c := colly.NewCollector()
	c.SetRequestTimeout(1 * time.Minute)

	c.OnHTML("tbody", func(e *colly.HTMLElement) {
		e.ForEach("tr", func(i int, t *colly.HTMLElement) {
			v, _ := t.DOM.Children().First().Attr("class")
			if v == "mediumhead" {
				println(t.ChildText(".mediumhead"))
			}
			if v == "rchars" {
				println(t.ChildText(".code"))
			}
			// t.ForEach(".mediumhead", func(i int, m *colly.HTMLElement) {
			// 	println(m.Text)
			// })
			// println(h.Text)
		})
		// for _, n := range e.F {
		// 	fmt.Printf("n. %+v\n", n.Data)
		// }
	})

	c.OnError(func(r *colly.Response, err error) {
		panic(err)
	})

	c.Visit("https://unicode.org/emoji/charts/full-emoji-list.html")
}
