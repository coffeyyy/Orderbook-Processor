package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"sync/atomic"
	"time"
	"aggregator"


	"github.com/gorilla/websocket"
	"github.com/emirpasic/gods/maps/hashmap"
)


func main() {

	aggregator.metricsLine.Store("N/A messages/sec | N/A updates/sec")
	book := &aggregator.OrderBook{
		Bids: hashmap.New(),
		Asks: hashmap.New(),
	}


	products := []string{"BTC-USD"}

	for _, product := range products {

		snapshotJSON := getOrderBookSnapshotHTTP(product)
		snapshot := mapSnapshot(snapshotJSON)

		for _, bid := range snapshot.Pricebook.Bids {
			book.Bids.Put(bid.Price, bid.Size)
		}
		for _, ask := range snapshot.Pricebook.Asks {
			book.Asks.Put(ask.Price, ask.Size)
		}

		log.Println("Snapshot loaded!")
		ctx := &aggregator.ProductContext{
			ProductID:  product,
			Book:       &aggregator.OrderBook{Bids: hashmap.New(), Asks: hashmap.New()},
			RawChan:    make(chan []byte, 100),
			UpdateChan: make(chan aggregator.L2Update, 50),
		}
		go startOrderBookPipeline(ctx)
	}
	startMetricsPrinter()
	select{}
}

func mapSnapshot(snapshotBytes string) aggregator.HTTPOrderBook {
	var snapshot aggregator.HTTPOrderBook

	json.Unmarshal([]byte(snapshotBytes), &snapshot)

	return snapshot
}

func getOrderBookSnapshotHTTP(productID string) string {
	var snapshot_url string = "https://api.coinbase.com/api/v3/brokerage/market/product_book"
	client := &http.Client{
		Timeout: time.Second * 10,
	}
	method := "GET"

	u, err := url.Parse(snapshot_url)
	if err != nil {
		log.Fatal(err)
	}

	params := url.Values{}
	params.Set("product_id", productID)
	u.RawQuery = params.Encode()

	request, err := http.NewRequest(method, u.String(), nil)

	if err != nil {
		log.Fatal(err)
	}

	request.Header.Set("Accept", "application/json")

	response, err := client.Do(request)

	if err != nil {
		log.Fatal(err)
	}

	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)

	if err != nil {
		log.Fatal(err)
	}

	return string(body)
}

func printOrderbook(productID string, book *aggregator.OrderBook, depth int) {

	fmt.Printf("============ Order Book %s ============\n", productID)
	fmt.Println("      ASK (price → qty)")

	// Extract, sort, and print asks (ascending)
	asks := extractKeys(book.Asks)
	sort.Slice(asks, func(i, j int) bool {
		return asks[i] < asks[j]
	})
	printDepth(asks, book.Asks, depth)

	fmt.Println("------------------------------------")

	// Extract, sort, and print bids (descending)
	fmt.Println("      BID (price → qty)")
	bids := extractKeys(book.Bids)
	sort.Slice(bids, func(i, j int) bool {
		return bids[i] > bids[j] // descending
	})
	printDepth(bids, book.Bids, depth)

	fmt.Println("===================================")
	if len(asks) > 0 && len(bids) > 0 {
		mid := (asks[0] + bids[0]) / 2
		fmt.Printf("Mid: %.2f\n", mid)
	} else {
		fmt.Println("Mid: N/A (insufficient data)")
	}
	if val := aggregator.metricsLine.Load(); val != nil {
		fmt.Println(val.(string))
	}
	fmt.Printf("\n")
	fmt.Printf("\n")
	fmt.Printf("\n")
	fmt.Printf("\n")
	fmt.Printf("\n")
}

func extractKeys(prices *hashmap.Map) []float64 {
	priceList := make([]float64, 0)
	for _, key := range prices.Keys() {
		switch k := key.(type) {
		case string:
			price, err := strconv.ParseFloat(k, 64)
			if err == nil {
				priceList = append(priceList, price)
			}
		case float64:
			priceList = append(priceList, k)
		case int:
			priceList = append(priceList, float64(k))
		default:

		}
	}
	return priceList
}

func printDepth(priceKeys []float64, orders *hashmap.Map, depth int) {
	for i := 0; i < len(priceKeys) && i < depth; i++ {
		price := priceKeys[i]
		priceStr := fmt.Sprintf("%.2f", price)
		if quantity, ok := orders.Get(priceStr); ok {
			fmt.Printf("Price: %.2f | Size: %v\n", price, quantity)
		}
	}
}

func startMetricsPrinter() {
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			msgs := atomic.SwapUint64(&aggregator.numMessages, 0)
			updates := atomic.SwapUint64(&aggregator.numUpdates, 0)

			if updates > atomic.LoadUint64(&aggregator.maxUpdatesPerSec) {
				atomic.StoreUint64(&aggregator.maxUpdatesPerSec, updates)
			}

			line := fmt.Sprintf("Total | %.2f messages/sec | %.2f updates/sec | max %.2f updates/sec",
				float64(msgs)/1.0, float64(updates)/1.0, float64(atomic.LoadUint64(&aggregator.maxUpdatesPerSec)))
			aggregator.metricsLine.Store(line)
		}
	}()
}

func startOrderBookUpdater(ctx *aggregator.ProductContext) {
	go func() {
		for update := range ctx.UpdateChan {
			for _, event := range update.Events {
				if event.Type != "update" {
					continue
				}
				for _, change := range event.Updates {
					price := change.PriceLevel
					quantity := change.NewQuantity

					switch change.Side {
					case "bid":
						if quantity == "0" {
							ctx.Book.Bids.Remove(price)
						} else {
							ctx.Book.Bids.Put(price, quantity)
						}
					case "offer":
						if quantity == "0" {
							ctx.Book.Asks.Remove(price)
						} else {
							ctx.Book.Asks.Put(price, quantity)
						}
					}
				}
				atomic.AddUint64(&aggregator.numUpdates, 1)
			}
			printOrderbook(ctx.ProductID, ctx.Book, 10)
			if val := aggregator.metricsLine.Load(); val != nil {
				fmt.Println(val.(string))
			}
		}
	}()
}

func startJSONDecoder(ctx *aggregator.ProductContext) {
	go func() {
		for msg := range ctx.RawChan {
			var update aggregator.L2Update
			if err := json.Unmarshal(msg, &update); err == nil && update.Channel == "l2_data" {
				select {
				case ctx.UpdateChan <- update:
				default:
					log.Printf("[%s] Dropping update - updateChan full", ctx.ProductID)
				}
			}
		}
	}()
}

func connectWebSocket(ctx *aggregator.ProductContext) {
	go func() {
		u := url.URL{Scheme: "wss", Host: "advanced-trade-ws.coinbase.com", Path: "/"}
		c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
		if err != nil {
			log.Fatalf("[%s] Dial error: %v", ctx.ProductID, err)
		}
		defer c.Close()

		// Subscribe
		subMsg := fmt.Sprintf(`{"type":"subscribe","channel":"level2","product_ids":["%s"]}`, ctx.ProductID)
		c.WriteMessage(websocket.TextMessage, []byte(subMsg))

		for {
			_, msg, err := c.ReadMessage()
			if err != nil {
				log.Printf("[%s] WebSocket read error: %v", ctx.ProductID, err)
				close(ctx.RawChan)
				return
			}
			atomic.AddUint64(&aggregator.numMessages, 1)
			select {
			case ctx.RawChan <- msg:
			default:
				log.Printf("[%s] Dropping raw message - rawChan full", ctx.ProductID)
			}
		}
	}()
}

func startOrderBookPipeline(ctx *aggregator.ProductContext) {
	connectWebSocket(ctx)
	startJSONDecoder(ctx)
	startOrderBookUpdater(ctx)
}
