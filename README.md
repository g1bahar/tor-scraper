# Tor Scraper

Tor ağı üzerinden .onion adreslerini toplu olarak tarayan Go uygulaması.



## Kurulum

```bash
go mod download
```

Tor Browser'ı açın veya Tor servisini başlatın.

## Kullanım

```bash
go run main.go targets.yaml
```

Veya derlenmiş binary ile:

```bash
go build -o tor-scraper.exe
./tor-scraper.exe targets.yaml
```

## Çıktılar

- scraped_data: İndirilen HTML dosyaları
- scan_report.log: Detaylı log dosyası

## Dosya Formatı

targets.yaml dosyası her satırda bir URL içermelidir:

```
http://duckduckgogg42xjoc72x3sjasowoarfbgcmvfimaftt6twagswzczad.onion
http://www.bbcnewsd73hkzno2ini43t4gblxvycyac5aw4gnv7t2rccijh7745uqd.onion
```

