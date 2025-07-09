package public

import (
	"embed"
	"errors"
	"fmt"
	"html"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os" // added for file existence check
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/komari-monitor/komari/database/models"
)

//go:embed dist
var PublicFS embed.FS

var DistFS fs.FS
var RawIndexFile string

var IndexFile string

func initIndex() {
	dist, err := fs.Sub(PublicFS, "dist")
	if err != nil {
		log.Println("Failed to create dist subdirectory:", err)
	}
	DistFS = dist

	indexFile, err := dist.Open("index.html")
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			log.Println("index.html not exist, you may forget to put dist of frontend to public/dist")
		}
		log.Println("Failed to open index.html:", err)
	}
	defer func() {
		_ = indexFile.Close()
	}()
	index, err := io.ReadAll(indexFile)
	if err != nil {
		log.Println("Failed to read index.html:", err)
	}
	RawIndexFile = string(index)
}
func UpdateIndex(cfg models.Config) {
	var titleReplacement string
	if cfg.Sitename == "Komari" {
		titleReplacement = "<title>Komari Monitor</title>"
	} else {
		titleReplacement = fmt.Sprintf("<title>%s</title>", html.EscapeString(cfg.Sitename))
	}

	replaceMap := map[string]string{
		"<title>Komari Monitor</title>": titleReplacement,
		"A simple server monitor tool.": cfg.Description,
		"</head>":                       cfg.CustomHead + "</head>",
		"</body>":                       cfg.CustomBody + "</body>",
	}
	updated := RawIndexFile
	for k, v := range replaceMap {
		updated = strings.Replace(updated, k, v, -1)
	}
	IndexFile = updated
}

func Static(r *gin.RouterGroup, noRoute func(handlers ...gin.HandlerFunc)) {
	initIndex()
	folders := []string{"assets", "images", "streamer", "static"}
	r.Use(func(c *gin.Context) {
		for i := range folders {
			if strings.HasPrefix(c.Request.RequestURI, fmt.Sprintf("/%s/", folders[i])) {
				c.Header("Cache-Control", "public, max-age=15552000")
			}
		}
	})
	for i, folder := range folders {
		sub, err := fs.Sub(DistFS, folder)
		if err != nil {
			log.Fatalf("can't find folder: %s", folder)
		}
		r.StaticFS(fmt.Sprintf("/%s/", folders[i]), http.FS(sub))
	}
	// Serve favicon.ico: use local file if exists, fallback to embedded
	r.GET("/favicon.ico", func(c *gin.Context) {
		if _, err := os.Stat("data/favicon.ico"); err == nil {
			c.File("data/favicon.ico")
			return
		}
		f, err := DistFS.Open("favicon.ico")
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		defer f.Close()
		data, err := io.ReadAll(f)
		if err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Data(http.StatusOK, "image/x-icon", data)
	})

	noRoute(func(c *gin.Context) {
		if c.Request.Method != "GET" && c.Request.Method != "POST" {
			c.Status(405)
			return
		}
		c.Header("Content-Type", "text/html")
		c.Status(200)

		c.Writer.WriteString(IndexFile)
		c.Writer.Flush()
		c.Writer.WriteHeaderNow()
	})
}
