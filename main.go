package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

const (
	torProxy9050 = "127.0.0.1:9050"
	torProxy9150 = "127.0.0.1:9150"
	timeout      = 60 * time.Second
	outputDir    = "scraped_data"
	logFile      = "scan_report.log"
)

type Scraper struct {
	client       *http.Client
	logFile      *os.File
	logger       *log.Logger
	successCount int
	failCount    int
}

func NewScraper() (*Scraper, error) {
	var dialer proxy.Dialer
	var err error

	dialer, err = proxy.SOCKS5("tcp", torProxy9150, nil, proxy.Direct)
	if err == nil {
		log.Println("[INFO] Tor Browser proxy (9150) kullanılıyor")
	} else {
		dialer, err = proxy.SOCKS5("tcp", torProxy9050, nil, proxy.Direct)
		if err == nil {
			log.Println("[INFO] Standalone Tor servisi proxy (9050) kullanılıyor")
		} else {
			return nil, fmt.Errorf("Tor proxy bağlantısı kurulamadı!\n"+
				"Lütfen şunları kontrol edin:\n"+
				"1. Tor Browser açık mı? (Port 9150)\n"+
				"2. Veya standalone Tor servisi çalışıyor mu? (Port 9050)\n"+
				"Hata: %v", err)
		}
	}

	transport := &http.Transport{
		Dial:              dialer.Dial,
		DisableKeepAlives: false,
		MaxIdleConns:      10,
		IdleConnTimeout:   90 * time.Second,
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("çok fazla yönlendirme (max 5)")
			}
			return nil
		},
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("çıktı klasörü oluşturulamadı: %v", err)
	}

	logFile, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("log dosyası oluşturulamadı: %v", err)
	}

	multiWriter := io.MultiWriter(os.Stdout, logFile)
	logger := log.New(multiWriter, "", log.LstdFlags)

	return &Scraper{
		client:  client,
		logFile: logFile,
		logger:  logger,
	}, nil
}

func (s *Scraper) Close() error {
	if s.logFile != nil {
		return s.logFile.Close()
	}
	return nil
}

func (s *Scraper) readURLs(filename string) ([]string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("dosya açılamadı: %v", err)
	}
	defer file.Close()

	var urls []string
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://") {
			urls = append(urls, line)
		} else if strings.Contains(line, ".onion") {
			if !strings.HasPrefix(line, "http://") {
				urls = append(urls, "http://"+line)
			} else {
				urls = append(urls, line)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("dosya okuma hatası: %v", err)
	}

	return urls, nil
}

func (s *Scraper) scrapeURL(url string) {
	s.logger.Printf("[INFO] Scanning: %s", url)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		s.logger.Printf("[ERR] Scanning: %s -> REQUEST_ERROR: %v", url, err)
		s.failCount++
		return
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")
	req.Header.Set("Accept-Encoding", "gzip, deflate")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Upgrade-Insecure-Requests", "1")

	resp, err := s.client.Do(req)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "timeout") || strings.Contains(errMsg, "deadline") {
			s.logger.Printf("[ERR] Scanning: %s -> TIMEOUT (Tor ağı yavaş olabilir veya site erişilemez)", url)
		} else if strings.Contains(errMsg, "connection refused") {
			s.logger.Printf("[ERR] Scanning: %s -> CONNECTION_REFUSED (Tor servisi çalışmıyor olabilir)", url)
		} else {
			s.logger.Printf("[ERR] Scanning: %s -> ERROR: %v", url, err)
		}
		s.failCount++
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		s.logger.Printf("[ERR] Scanning: %s -> HTTP_%d", url, resp.StatusCode)
		s.failCount++
		return
	}

	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		location := resp.Header.Get("Location")
		if location != "" {
			s.logger.Printf("[INFO] Scanning: %s -> HTTP_%d (Redirect to: %s)", url, resp.StatusCode, location)
		} else {
			s.logger.Printf("[INFO] Scanning: %s -> HTTP_%d (Redirect)", url, resp.StatusCode)
		}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		s.logger.Printf("[ERR] Scanning: %s -> READ_ERROR: %v", url, err)
		s.failCount++
		return
	}

	filename := sanitizeFilename(url)
	filepath := filepath.Join(outputDir, filename+".html")

	if err := os.WriteFile(filepath, body, 0644); err != nil {
		s.logger.Printf("[ERR] Scanning: %s -> SAVE_ERROR: %v", url, err)
		s.failCount++
		return
	}

	s.logger.Printf("[SUCCESS] Scanning: %s -> SUCCESS (Saved: %s)", url, filepath)
	s.successCount++
}

func sanitizeFilename(url string) string {
	filename := strings.ReplaceAll(url, "http://", "")
	filename = strings.ReplaceAll(filename, "https://", "")
	filename = strings.ReplaceAll(filename, "/", "_")
	filename = strings.ReplaceAll(filename, ":", "_")
	filename = strings.ReplaceAll(filename, ".", "_")
	return filename
}

func (s *Scraper) verifyTorIP() error {
	s.logger.Println("[INFO] Tor IP doğrulaması yapılıyor...")

	resp, err := s.client.Get("https://check.torproject.org/api/ip")
	if err != nil {
		return fmt.Errorf("Tor IP kontrolü başarısız: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("Tor IP yanıtı okunamadı: %v", err)
	}

	s.logger.Printf("[INFO] Tor IP Doğrulama: %s", string(body))
	return nil
}

func (s *Scraper) generateReport() {
	s.logger.Println("\n" + strings.Repeat("=", 60))
	s.logger.Println("TARAMA RAPORU")
	s.logger.Println(strings.Repeat("=", 60))
	s.logger.Printf("Başarılı: %d", s.successCount)
	s.logger.Printf("Başarısız: %d", s.failCount)
	s.logger.Printf("Toplam: %d", s.successCount+s.failCount)
	s.logger.Printf("Çıktı klasörü: %s", outputDir)
	s.logger.Printf("Log dosyası: %s", logFile)
	s.logger.Println(strings.Repeat("=", 60))
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Kullanım: go run main.go <targets.yaml>")
		fmt.Println("Örnek: go run main.go targets.yaml")
		os.Exit(1)
	}

	targetsFile := os.Args[1]

	scraper, err := NewScraper()
	if err != nil {
		log.Fatalf("Scraper oluşturulamadı: %v", err)
	}
	defer scraper.Close()

	if err := scraper.verifyTorIP(); err != nil {
		scraper.logger.Printf("[WARN] %v", err)
		scraper.logger.Println("[WARN] Devam ediliyor, ancak Tor bağlantısı doğrulanamadı")
	}

	scraper.logger.Printf("[INFO] Dosya okunuyor: %s", targetsFile)
	urls, err := scraper.readURLs(targetsFile)
	if err != nil {
		log.Fatalf("URL listesi okunamadı: %v", err)
	}

	scraper.logger.Printf("[INFO] %d URL bulundu", len(urls))
	scraper.logger.Println(strings.Repeat("-", 60))

	for i, url := range urls {
		scraper.logger.Printf("[INFO] İlerleme: %d/%d", i+1, len(urls))
		scraper.scrapeURL(url)

		if i < len(urls)-1 {
			time.Sleep(3 * time.Second)
		}
	}

	scraper.generateReport()
}
