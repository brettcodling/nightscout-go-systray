# nightscout-go-systray
A Linux system tray app to display nightscout data

## build
```
go get -u ./...
go build .
```

## running
```
Usage of ./cgm:
  -high float
        Your BG high target (default 8)
  -low float
        Your BG low target (default 4)
  -urgent-high float
        Your BG urgent high target (default 15)
  -url string
        Your nightscout url e.g. https://example.herokuapp.com
```
