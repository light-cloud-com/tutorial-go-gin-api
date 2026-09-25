package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
)

type Link struct {
	ID  int    `json:"id"`
	URL string `json:"url"`
}

type newLink struct {
	URL string `json:"url" binding:"required,url"`
}

// store keeps links in memory: fine for a demo, gone on every restart.
type store struct {
	mu    sync.Mutex
	links []Link
}

func (s *store) add(url string) Link {
	s.mu.Lock()
	defer s.mu.Unlock()
	link := Link{ID: len(s.links) + 1, URL: url}
	s.links = append(s.links, link)
	return link
}

func (s *store) get(id int) (Link, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id < 1 || id > len(s.links) {
		return Link{}, false
	}
	return s.links[id-1], true
}

func main() {
	// JSON logs on stdout. The Logs tab reads "severity" and "message",
	// so rename slog's "level" and "msg" keys.
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			switch a.Key {
			case slog.LevelKey:
				a.Key = "severity"
			case slog.MessageKey:
				a.Key = "message"
			}
			return a
		},
	}))

	links := &store{}
	router := gin.New()
	router.Use(gin.Recovery())

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	router.POST("/links", func(c *gin.Context) {
		var body newLink
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "send {\"url\": \"https://...\"}"})
			return
		}
		link := links.add(body.URL)
		logger.Info("link created", "id", link.ID, "url", link.URL)
		c.JSON(http.StatusCreated, link)
	})

	// /l/3 redirects to the link with id 3.
	router.GET("/l/:id", func(c *gin.Context) {
		id, _ := strconv.Atoi(c.Param("id"))
		link, ok := links.get(id)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "link not found"})
			return
		}
		c.Redirect(http.StatusFound, link.URL)
	})

	// Light Cloud tells the app which port to use in PORT.
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	server := &http.Server{Addr: ":" + port, Handler: router}

	// Stop cleanly on SIGTERM, which Light Cloud sends to old instances.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Info("listening", "port", port, "mode", gin.Mode())
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server stopped", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
}
