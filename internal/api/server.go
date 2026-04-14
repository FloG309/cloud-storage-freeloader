package api

import (
	"context"
	"io"
	"net/http"

	"github.com/FloG309/cloud-storage-freeloader/internal/vfs"
	"github.com/gin-gonic/gin"
)

// Server is the REST API server.
type Server struct {
	vfs    *vfs.VFS
	router *gin.Engine
}

// NewServer creates an API server backed by the given VFS.
func NewServer(v *vfs.VFS) *Server {
	gin.SetMode(gin.TestMode)
	s := &Server{vfs: v}
	s.setupRoutes()
	return s
}

// Router returns the gin engine for testing.
func (s *Server) Router() *gin.Engine {
	return s.router
}

func (s *Server) setupRoutes() {
	r := gin.New()

	api := r.Group("/api")
	{
		api.GET("/files", s.listFiles)
		api.POST("/files", s.uploadFile)
		api.GET("/files/download", s.downloadFile)
		api.DELETE("/files", s.deleteFile)
		api.PATCH("/files", s.renameFile)
		api.GET("/status", s.status)
		api.GET("/providers", s.providers)
	}

	s.router = r
}

func (s *Server) listFiles(c *gin.Context) {
	path := c.DefaultQuery("path", "/")
	entries, err := s.vfs.ReadDir(context.Background(), path)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, entries)
}

func (s *Server) uploadFile(c *gin.Context) {
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no file in request"})
		return
	}
	defer file.Close()

	path := c.PostForm("path")
	if path == "" {
		path = "/" + header.Filename
	}

	data, _ := io.ReadAll(file)
	err = s.vfs.Write(context.Background(), path, readerFromBytes(data), int64(len(data)))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"path": path, "size": len(data)})
}

func (s *Server) downloadFile(c *gin.Context) {
	path := c.Query("path")
	if path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path required"})
		return
	}

	info, err := s.vfs.Stat(context.Background(), path)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
		return
	}

	data, err := s.vfs.Read(context.Background(), path, 0, info.Size)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Data(http.StatusOK, "application/octet-stream", data)
}

func (s *Server) deleteFile(c *gin.Context) {
	path := c.Query("path")
	if err := s.vfs.Delete(context.Background(), path); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": path})
}

func (s *Server) renameFile(c *gin.Context) {
	path := c.Query("path")

	var body struct {
		NewPath string `json:"newPath"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}

	if err := s.vfs.Rename(context.Background(), path, body.NewPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"renamed": body.NewPath})
}

func (s *Server) status(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
	})
}

func (s *Server) providers(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"providers": []interface{}{},
	})
}

type bytesReader struct {
	data []byte
	pos  int
}

func readerFromBytes(data []byte) io.Reader {
	return &bytesReader{data: data}
}

func (r *bytesReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}
