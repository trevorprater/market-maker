package main

import (
	"fmt"
	"log"
	"time"

	"github.com/zoh/go-bittrex"

	"github.com/trevorprater/crypto/bittrex/orderbook"
	"github.com/trevorprater/crypto/bittrex/util"
)

func main() {
	var client = bittrex.New("", "")
	var orderBooks = make(map[string]*orderbook.OrderBook)
	var exchangeEventChan = make(chan bittrex.ExchangeEvent)
	var stopChan = make(chan bool)

	markets, err := client.GetMarkets()
	marketNames := make([]string, 0)
	if err != nil {
		panic(err)
	}

	for i := range markets {
		orderbook := orderbook.New(&markets[i])
		orderBooks[markets[i].MarketName] = orderbook
		marketNames = append(marketNames, markets[i].MarketName)
	}

	for i := 0; i < 250; i++ {
		go func() {
			for {
				select {
				case data := <-exchangeEventChan:
					go orderBooks[data.State.MarketName].InsertEvent(&data)
				}
			}
		}()
	}

	go func() {
		err = client.SubscribeExchangeUpdate(marketNames, "", exchangeEventChan, stopChan)
		if err != nil {
			log.Panicln(err)
			panic(err)
		}
	}()

	for {
		spreads := make(map[string]float64)
		var ss []*util.KV

		for i := range marketNames {
			//volume := orderBooks[marketNames[i]].VolumeSince(time.Minute * 5)
			spread := orderBooks[marketNames[i]].Spread(0.01)
			spreads[marketNames[i]] = spread
			ss = append(ss, &util.KV{marketNames[i], int(spread * 1000)})
		}

		ss = util.SortMap(ss)
		fmt.Printf("\n   MKT      SPREAD(%s)   NUM_3MIN_FILLS            BID                             ASK\n\n", "%")
		for _, kv := range ss {
			if spreads[kv.Key] > 0.5 {
				fillsSince := orderBooks[kv.Key].FilterFillsByTime(time.Minute * 3)
				if len(fillsSince) > 1 {
					bestAsk := orderBooks[kv.Key].BestAsksN(1)[0]
					bestBid := orderBooks[kv.Key].BestBidsN(1)[0]

					fmt.Printf(" %-9s    %-.2f%s      %-2v              %-10.8g  %-10.8g           %-10.8g  %-10.8g\n", kv.Key, spreads[kv.Key], "", len(fillsSince), bestBid[0], bestBid[1], bestAsk[0], bestAsk[1])
				}
			}
		}
		time.Sleep(time.Millisecond * 5000)
		fmt.Println()
	}
}
