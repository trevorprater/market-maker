package orderbook

import (
	"errors"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/trevorprater/crypto/bittrex/util"
	"github.com/zoh/go-bittrex"
)

var client = bittrex.New("", "")

// OrderBook manages the orders for a market
type OrderBook struct {
	Market *bittrex.Market     `json:"market"`
	Bids   map[float64]float64 `json:"bids"`
	Asks   map[float64]float64 `json:"asks"`
	Fills  []bittrex.Fill     `json:"fills"`
	Events []*bittrex.ExchangeEvent

	bidRatesSorted []float64
	askRatesSorted []float64

	bidMutex    *sync.RWMutex
	askMutex    *sync.RWMutex
	fillMutex   *sync.RWMutex
	eventsMutex *sync.RWMutex
}

// New initializes and returns a new OrderBook for a given Market.
func New(market *bittrex.Market) *OrderBook {
	var o OrderBook
	o.Market = market

	o.Bids = make(map[float64]float64)
	o.Asks = make(map[float64]float64)
	o.Fills = make([]bittrex.Fill, 0)
	o.Events = make([]*bittrex.ExchangeEvent, 0)

	o.bidRatesSorted = make([]float64, 0)
	o.askRatesSorted = make([]float64, 0)

	o.bidMutex = &sync.RWMutex{}
	o.askMutex = &sync.RWMutex{}
	o.fillMutex = &sync.RWMutex{}
	o.eventsMutex = &sync.RWMutex{}

	go o.ingestMarketData()

	return &o
}

func (o *OrderBook) ingestMarketData() {
	o.LockAll()
	defer o.UnlockAll()

	book, err := client.GetOrderBook(o.Market.MarketName, "both", 100)
	if err != nil {
		panic(err)
	}
	for i := range book.Buy {
		rate, _ := book.Buy[i].Rate.Float64()
		qty, _ := book.Buy[i].Quantity.Float64()
		if qty != util.Zero {
			o.Bids[rate] = qty
		}
	}
	bidKeys := make([]float64, len(o.Bids))
	i := 0
	for k := range o.Bids {
		bidKeys[i] = k
		i++
	}
	sort.Float64s(bidKeys)
	o.bidRatesSorted = bidKeys

	for i := range book.Sell {
		rate, _ := book.Sell[i].Rate.Float64()
		qty, _ := book.Sell[i].Quantity.Float64()
		if qty != util.Zero {
			o.Asks[rate] = qty
		}
	}
	askKeys := make([]float64, len(o.Asks))
	i = 0
	for k := range o.Asks {
		askKeys[i] = k
		i++
	}
	sort.Float64s(askKeys)
	o.askRatesSorted = askKeys
}

//InsertEvent inserts bids/asks/fills.
func (o *OrderBook) InsertEvent(event *bittrex.ExchangeEvent) {
	o.eventsMutex.Lock()
	o.Events = append(o.Events, event)
	o.eventsMutex.Unlock()
	o.insertBids(event.State.Buys)
	o.insertAsks(event.State.Sells)
	o.insertFills(event.State.Fills)
}

func (o *OrderBook) insertBids(bids []bittrex.OrderUpdate) {
	o.bidMutex.Lock()
	defer o.bidMutex.Unlock()

	for i := range bids {
		rate, _ := bids[i].Rate.Float64()
		qty, _ := bids[i].Quantity.Float64()
		if qty != util.Zero {
			o.Bids[rate] = qty
		} else {
			delete(o.Bids, rate)
		}
	}
	keys := make([]float64, len(o.Bids))
	i := 0
	for k := range o.Bids {
		keys[i] = k
		i++
	}
	sort.Float64s(keys)
	o.bidRatesSorted = keys
}

func (o *OrderBook) insertAsks(asks []bittrex.OrderUpdate) {
	o.askMutex.Lock()
	defer o.askMutex.Unlock()
	for i := range asks {
		rate, _ := asks[i].Rate.Float64()
		qty, _ := asks[i].Quantity.Float64()
		if qty != util.Zero {
			o.Asks[rate] = qty
		} else {
			delete(o.Asks, rate)
		}
	}
	keys := make([]float64, len(o.Asks))
	i := 0
	for k := range o.Asks {
		keys[i] = k
		i++
	}
	sort.Float64s(keys)
	o.askRatesSorted = keys
}

func (o *OrderBook) insertFills(fills []bittrex.Fill) {
	o.fillMutex.Lock()
	defer o.fillMutex.Unlock()
	for i := range fills {
		o.Fills = append([]bittrex.Fill{fills[i]}, o.Fills...)
	}
}

// FilterFillsByTime returns all fills that occured since time, t.
func (o *OrderBook) FilterFillsByTime(t time.Duration) []bittrex.Fill {
	var fills = make([]bittrex.Fill, 0)
	o.fillMutex.RLock()
	defer o.fillMutex.RUnlock()
	for i := range o.Fills {
		if o.Fills[i].Timestamp.UTC().After(time.Now().UTC().Add(-t)) {
			fills = append(fills, o.Fills[i])
		} else {
			break
		}
	}
	return fills
}

// AvgRateSince returns the average market rate since time, t.
// In an effort to reduce unncessary Decimal->float64 conversions, code is kinda ugly.
func (o *OrderBook) AvgRateSince(t time.Duration) float64 {
	rate := 0.0
	totalBaseCurrencyTransacted := 0.0

	var convertedRates = make([]float64, 0)
	var amountsTransactedBaseCurrency = make([]float64, 0)

	fills := o.FilterFillsByTime(t)
	if len(fills) == 0 {
		log.Printf(
			"Warning: Not enough data for AvgMarketPrice; calling API for MarketSummary (%s)\n",
			o.Market.MarketName)
		summary, err := client.GetMarketSummary(o.Market.MarketName)
		if err != nil {
			panic(err)
		}
		return summary[0].Last
	}
	for i := range fills {
		rateFloat, _ := fills[i].Rate.Float64()
		qtyFloat, _ := fills[i].Quantity.Float64()

		amountTransacted := rateFloat * qtyFloat
		convertedRates = append(convertedRates, rateFloat)
		amountsTransactedBaseCurrency = append(amountsTransactedBaseCurrency, amountTransacted)
		totalBaseCurrencyTransacted += amountTransacted
	}
	for i := range fills {
		weight := amountsTransactedBaseCurrency[i] / totalBaseCurrencyTransacted
		rate += weight * convertedRates[i]
	}
	return rate
}

// VolumeSince returns the total volume transacted since time, t.
func (o *OrderBook) VolumeSince(t time.Duration) float64 {
	fills := o.FilterFillsByTime(t)
	volume := 0.0

	for i := range fills {
		v, _ := fills[i].Quantity.Mul(fills[i].Rate).Float64()
		volume += v
	}
	return volume
}

// Spread returns the percent diff between bid/ask at specified depth (baseCurrency).
// Example: Return the bid/ask spread for BTC-XRP where the rates
//			are obtained from depth, 0.005 BTC, on each book.
func (o *OrderBook) Spread(priceDepth float64) float64 {
	bid, err := o.BidRate(priceDepth)
	if err != nil {
		return -1.0
	}
	ask, err := o.AskRate(priceDepth)
	if err != nil {
		return -1.0
	}
	return 100.0 * (ask - bid) / ask
}

// SpreadAfterFees returns the percent diff between bid/ask after fees.
func (o *OrderBook) SpreadAfterFees(priceDepth float64) float64 {
	return o.Spread(priceDepth) - 0.5
}

// BidRate returns the bid rate at the specified depth.
func (o *OrderBook) BidRate(priceDepth float64) (float64, error) {
	bids, _ := o.BestBidsDepth(priceDepth)
	if len(bids) > 0 {
		return bids[len(bids)-1][0], nil
	} else {
		return -1.0, errors.New("no bids returned")
	}
}

// AskRate returns the ask rate at the specified depth in base currency.
func (o *OrderBook) AskRate(priceDepth float64) (float64, error) {
	asks, _ := o.BestAsksDepth(priceDepth)
	if len(asks) > 0 {
		return asks[0][0], nil
	} else {
		return -1.0, errors.New("no asks returned")
	}
}

// BestBidsN returns nth best bids.
func (o *OrderBook) BestBidsN(n int) [][]float64 {
	bids := make([][]float64, n)
	numBids := len(o.bidRatesSorted)

  	o.bidMutex.RLock()
  	defer o.bidMutex.RUnlock()
	for i := 0; i < n; i++ {
		if numBids > i {
			rate := o.bidRatesSorted[numBids-i-1]
			bids[i] = []float64{rate, o.Bids[rate]}
		} else {
			bids[i] = []float64{0.0, 0.0}
		}
	}
	return bids
}

// BestAsksN returns the nth best ask orders
func (o *OrderBook) BestAsksN(n int) [][]float64 {
	asks := make([][]float64, n)
	o.askMutex.RLock()
	defer o.askMutex.RUnlock()
	for i := 0; i < n; i++ {
		if len(o.askRatesSorted) > i {
			rate := o.askRatesSorted[i]
			asks[i] = []float64{rate, o.Asks[rate]}
		} else {
			asks[i] = []float64{0.0, 0.0}
		}
	}
	return asks
}


// BestBidsDepth returns the best bid orders up to depth in base currency.
// Example: Return the top of the ETH-XRP order book up to 0.05 ETH
func (o *OrderBook) BestBidsDepth(priceDepth float64) ([][]float64, error) {
	bids := make([][]float64, 0)

	o.bidMutex.RLock()
	defer o.bidMutex.RUnlock()
	totalBids := len(o.bidRatesSorted)
	for i := 0; i < totalBids; i++ {
		rate := o.bidRatesSorted[totalBids-i-1]
		if priceDepth >= 0.0 {
			bids = append(bids, []float64{rate, o.Bids[rate]})
			priceDepth -= rate * o.Bids[rate]
		} else {
			break
		}
	}
	return bids, nil
}

// BestAsksDepth returns the best ask orders up to depth in base currency.
// Example: Return the top ETH-XRP ask limit orders up to 0.05 ETH
func (o *OrderBook) BestAsksDepth(priceDepth float64) ([][]float64, error) {
	asks := make([][]float64, 0)
	o.askMutex.RLock()
	defer o.askMutex.RUnlock()
	for i := range o.askRatesSorted {
		rate := o.askRatesSorted[i]
		if priceDepth >= 0.0 {
			asks = append(asks, []float64{rate, o.Asks[rate]})
			priceDepth -= rate * o.Bids[rate]
		} else {
			break
		}
	}
	if len(asks) < 1 {
		return asks, errors.New("no asks available")
	}
	return asks, nil
}

//LockAll acquires every lock associated with OrderBook
func (o *OrderBook) LockAll() {
	o.askMutex.Lock()
	o.bidMutex.Lock()
	o.eventsMutex.Lock()
	o.fillMutex.Lock()
}

//UnlockAll releases every lock associated with OrderBook
func (o *OrderBook) UnlockAll() {
	o.askMutex.Unlock()
	o.bidMutex.Unlock()
	o.eventsMutex.Unlock()
	o.fillMutex.Unlock()
}
