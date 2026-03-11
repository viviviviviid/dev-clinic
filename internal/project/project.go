package project

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

type Status struct {
	Loaded      bool   `json:"loaded"`
	Dir         string `json:"dir"`
	Language    string `json:"language"`
	CurrentStep string `json:"currentStep"`
	Goal        string `json:"goal"`
	Concept     string `json:"concept"`
	Tasks       string `json:"tasks"`
	Content     string `json:"content"`
	SkillLevel  string `json:"skillLevel"`
}

type Manager struct {
	mu      sync.RWMutex
	dir     string
	content string
	loaded  bool
}

var Global = &Manager{}

func (m *Manager) Load(dir string) error {
	path := filepath.Join(dir, "TUTORSYS.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dir = dir
	m.content = string(data)
	m.loaded = true
	return nil
}

func (m *Manager) Set(dir, content string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dir = dir
	m.content = content
	m.loaded = true
}

func (m *Manager) GetStatus() Status {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.loaded {
		return Status{Loaded: false}
	}
	return Status{
		Loaded:      true,
		Dir:         m.dir,
		Language:    extractSection(m.content, "언어 & 환경"),
		CurrentStep: extractSection(m.content, "현재 단계"),
		Goal:        extractSection(m.content, "학습자 목표"),
		Concept:     extractSection(m.content, "개념 설명"),
		Tasks:       extractSection(m.content, "현재 과제"),
		Content:     m.content,
		SkillLevel:  extractSection(m.content, "학습 수준"),
	}
}

func (m *Manager) GetDir() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.dir
}

func (m *Manager) GetContent() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.content
}

func (m *Manager) IsLoaded() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.loaded
}

func (m *Manager) GetSkillLevel() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return extractSection(m.content, "학습 수준")
}

func (m *Manager) UpdateCurrentStep(step string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		return fmt.Errorf("project not loaded")
	}
	re := regexp.MustCompile(`(?m)^## 현재 단계\n.*$`)
	m.content = re.ReplaceAllString(m.content, "## 현재 단계\n"+step)

	// mark step complete in curriculum
	stepRe := regexp.MustCompile(`(?m)^- \[ \] ` + regexp.QuoteMeta(step))
	m.content = stepRe.ReplaceAllString(m.content, "- [x] "+step)

	path := filepath.Join(m.dir, "TUTORSYS.md")
	return os.WriteFile(path, []byte(m.content), 0644)
}

func extractSection(content, header string) string {
	lines := strings.Split(content, "\n")
	inSection := false
	var result []string
	for _, line := range lines {
		if strings.HasPrefix(line, "## "+header) {
			inSection = true
			continue
		}
		if inSection {
			if strings.HasPrefix(line, "## ") {
				break
			}
			result = append(result, line)
		}
	}
	return strings.TrimSpace(strings.Join(result, "\n"))
}
