package parser

import (
	"context"
	"testing"

	"github.com/yaselnik/Search-Engine/internal/domain"
)

func TestParser_SinglePassExtraction(t *testing.T) {
	htmlContent := `<!DOCTYPE html>
<html lang="ru-RU">
<head>
	<title>  Поисковая   система  </title>
	<base href="https://example.com/docs/v1/">
	<style>body { color: black; }</style>
</head>
<body>
	<nav><a href="menu.html">Меню</a></nav>
	<main>
		<h1>Заголовок</h1>
		<p>Первый   абзац
		текста.</p>
		<a href="../about.html">О проекте</a>
		<a href="https://external.org/page" rel="nofollow">Реклама</a>
	</main>
	<script>console.log("ignored");</script>
</body>
</html>`

	p := NewParser()
	res, err := p.Parse(context.Background(), &domain.FetchedPage{
		URL:      "https://example.com/index.html",
		FinalURL: "https://example.com/index.html",
		Body:     []byte(htmlContent),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.Language != "ru" {
		t.Errorf("expected language 'ru', got %q", res.Language)
	}
	if res.Title != "Поисковая система" {
		t.Errorf("expected clean title, got %q", res.Title)
	}
	// Text from <nav>, <script>, and <style> must be excluded; whitespace collapsed.
	if res.Text != "Заголовок Первый абзац текста. О проекте Реклама" {
		t.Errorf("unexpected extracted text: %q", res.Text)
	}
	// Link in <nav> and <main> must be resolved against <base href>, while rel="nofollow" is skipped.
	if len(res.Links) != 2 {
		t.Fatalf("expected 2 links, got %v", res.Links)
	}
	if res.Links[0] != "https://example.com/docs/v1/menu.html" || res.Links[1] != "https://example.com/docs/about.html" {
		t.Errorf("unexpected resolved links: %v", res.Links)
	}
}

func TestParser_RobotsMetaDirectives(t *testing.T) {
	htmlContent := `<html>
<head><meta name="robots" content="noindex, nofollow"></head>
<body><p>Секретный текст</p><a href="/link">Ссылка</a></body>
</html>`

	p := NewParser()
	res, err := p.Parse(context.Background(), &domain.FetchedPage{
		URL:      "https://example.com/secret",
		FinalURL: "https://example.com/secret",
		Body:     []byte(htmlContent),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.NoIndex {
		t.Errorf("expected NoIndex=true")
	}
	if len(res.Links) != 0 {
		t.Errorf("expected 0 links when nofollow is set, got %v", res.Links)
	}
}
