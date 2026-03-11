package api

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type GotoReq struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Col  int    `json:"col"`
}

type GotoResp struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Col  int    `json:"col"`
}

var gotoPattern = regexp.MustCompile(`^(.+):(\d+):(\d+)`)

// GotoDefinition runs gopls to find a symbol's definition location.
// POST /api/goto
func GotoDefinition(c *gin.Context) {
	var req GotoReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	location := fmt.Sprintf("%s:%d:%d", req.File, req.Line, req.Col)
	out, err := exec.CommandContext(ctx, "gopls", "definition", location).Output()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"error": "gopls failed: " + err.Error()})
		return
	}

	line := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	m := gotoPattern.FindStringSubmatch(line)
	if m == nil {
		c.JSON(http.StatusOK, gin.H{"error": "could not parse gopls output"})
		return
	}

	lineNum, _ := strconv.Atoi(m[2])
	colNum, _ := strconv.Atoi(m[3])

	c.JSON(http.StatusOK, GotoResp{
		File: m[1],
		Line: lineNum,
		Col:  colNum,
	})
}
