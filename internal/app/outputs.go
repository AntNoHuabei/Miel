package app

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

// 产物输出目录结构(均在应用数据目录下):
//
//	outputs/
//	  reports/   周报
//	  documents/ 文档(markdown)
//	  tables/    表格(CSV)
func outputDir(sub string) string {
	dir, err := appDirectories.OutputDirectory(sub)
	if err != nil {
		fmt.Println("create output dir failed:", err)
		return appDirectories.Path(DirectoryOutputs)
	}
	return dir
}

// nonWord 匹配非文字/数字/连接符字符(用于文件名化)。
var nonWord = regexp.MustCompile(`[^\p{L}\p{N}_-]+`)

// slugify 把标题转成安全的文件名片段。
func slugify(s string) string {
	s = strings.TrimSpace(s)
	s = nonWord.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "untitled"
	}
	if r := []rune(s); len(r) > 40 {
		s = string(r[:40])
	}
	return s
}

// stamp 返回可用于文件名的本地时间戳。
func stamp() string { return time.Now().Format("20060102_150405") }

// writeOutput 写入产物文件并返回路径。
func writeOutput(sub, filename string, content []byte) (string, error) {
	path, err := appDirectories.OutputPath(sub, filename)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return "", fmt.Errorf("写入文件失败: %w", err)
	}
	return path, nil
}

// DataDir 返回应用数据目录(设置页展示与打开,便于用户找到导出产物)。
func (s *SettingsService) DataDir() string { return dataDir() }

// OpenDataDir 在系统文件管理器中打开应用数据目录。
func (s *SettingsService) OpenDataDir() error { return appDirectories.OpenRoot() }
