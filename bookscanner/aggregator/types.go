package aggregator

import (
		"github.com/emirpasic/gods/maps/hashmap"
		"sync"
		"time"
		"sync/atomic"
)

var metricsLine atomic.Value
var numMessages uint64
var numUpdates uint64
var maxUpdatesPerSec uint64

type Venue string

type Side uint8

const (
	Bid Side = iota
	Ask
)

type HTTPOrderBook struct {
	Pricebook Pricebook `json:"pricebook"`
}

func (s Side) String() string {
	switch s {
	case Bid:
		return "bid"
	case Ask:
		return "ask"
	default:
		return "unknown"
	}
}

type Level struct {
	
}

type Update struct {
	Venue string
	Symbol string
	SeqID uint64
	Timestamp time.Time
	Price float64
	Qty float64
	Side uint8
}

type Pricebook struct {
	ProductID string            `json:"product_id"`
	Bids      []PriceLevelEntry `json:"bids"`
	Asks      []PriceLevelEntry `json:"asks"`
}

type PriceLevelEntry struct {
	Price string `json:"price"`
	Size  string `json:"size"`
}

type Snapshot struct {
	Type      string     `json:"type"`
	ProductID string     `json:"product_id"`
	Bids      [][]string `json:"bids"`
	Asks      [][]string `json:"asks"`
}

type L2Update struct {
	Channel string    `json:"channel"`
	Events  []L2Event `json:"events"`
}

type L2Event struct {
	Type      string          `json:"type"`
	ProductID string          `json:"product_id"`
	Updates   []L2PriceUpdate `json:"updates"`
}

type L2PriceUpdate struct {
	Side        string `json:"side"`
	PriceLevel  string `json:"price_level"`
	NewQuantity string `json:"new_quantity"`
}

type OrderBook struct {
	Bids *hashmap.Map
	Asks *hashmap.Map
	sync.Mutex
}

type ProductContext struct {
	Venue string
	ProductID   string
	Book        *OrderBook
	RawChan     chan []byte
	UpdateChan  chan L2Update
}