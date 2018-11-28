// Package connectable provides a Connectable and its methods.
package connectable

import (
	"sync"
	"time"

	"github.com/reactivex/rxgo"
	"github.com/reactivex/rxgo/fx"
	"github.com/reactivex/rxgo/handlers"
	"github.com/reactivex/rxgo/observable"
	"github.com/reactivex/rxgo/observer"
	"github.com/reactivex/rxgo/subscription"
)

// Connectable can subscribe to several EventHandlers
// before starting processing with Connect.
type Connectable interface {
	Connect() <-chan (chan subscription.Subscription)
	Do(nextf func(interface{})) Connectable
	Subscribe(handler rx.EventHandler, opts ...observable.Option) Connectable
	Map(fn fx.MappableFunc) Connectable
	Filter(fn fx.FilterableFunc) Connectable
	Scan(apply fx.ScannableFunc) Connectable
	First() Connectable
	Last() Connectable
	Distinct(apply fx.KeySelectorFunc) Connectable
	DistinctUntilChanged(apply fx.KeySelectorFunc) Connectable
}

type connector struct {
	observable.Observable
	observers []observer.Observer
}

// New creates a Connectable with optional observer(s) as parameters.
func New(buffer uint, observers ...observer.Observer) Connectable {
	return &connector{
		Observable: observable.New(buffer),
		observers:  observers,
	}
}

// From creates a Connectable from an Iterator.
func From(it rx.Iterator) Connectable {
	source := make(chan interface{})
	go func() {
		for {
			val, err := it.Next()
			if err != nil {
				break
			}
			source <- val
		}
		close(source)
	}()
	return &connector{
		Observable: observable.NewFromChannel(source),
	}
}

// Empty creates a Connectable with no item and terminate immediately.
func Empty() Connectable {
	source := make(chan interface{})
	go func() {
		close(source)
	}()
	return &connector{
		Observable: observable.NewFromChannel(source),
	}
}

// Interval creates a Connectable emitting incremental integers infinitely between
// each given time interval.
func Interval(term chan struct{}, timeout time.Duration) Connectable {
	source := make(chan interface{})
	go func(term chan struct{}) {
		i := 0
	OuterLoop:
		for {
			select {
			case <-term:
				break OuterLoop
			case <-time.After(timeout):
				source <- i
			}
			i++
		}
		close(source)
	}(term)

	return &connector{
		Observable: observable.NewFromChannel(source),
	}
}

// Range creates an Connectable that emits a particular range of sequential integers.
func Range(start, end int) Connectable {
	source := make(chan interface{})
	go func() {
		i := start
		for i < end {
			source <- i
			i++
		}
		close(source)
	}()
	return &connector{
		Observable: observable.NewFromChannel(source),
	}
}

// Just creates an Connectable with the provided item(s).
func Just(item interface{}, items ...interface{}) Connectable {
	source := make(chan interface{})
	if len(items) > 0 {
		items = append([]interface{}{item}, items...)
	} else {
		items = []interface{}{item}
	}

	go func() {
		for _, item := range items {
			source <- item
		}
		close(source)
	}()

	return &connector{
		Observable: observable.NewFromChannel(source),
	}
}

// Start creates a Connectable from one or more directive-like EmittableFunc
// and emits the result of each operation asynchronously on a new Connectable.
func Start(f fx.EmittableFunc, fs ...fx.EmittableFunc) Connectable {
	if len(fs) > 0 {
		fs = append([]fx.EmittableFunc{f}, fs...)
	} else {
		fs = []fx.EmittableFunc{f}
	}

	source := make(chan interface{})

	var wg sync.WaitGroup
	for _, f := range fs {
		wg.Add(1)
		go func(f fx.EmittableFunc) {
			source <- f()
			wg.Done()
		}(f)
	}

	go func() {
		wg.Wait()
		close(source)
	}()

	return &connector{
		Observable: observable.NewFromChannel(source),
	}
}

// Subscribe subscribes an EventHandler and returns a Connectable.
func (c *connector) Subscribe(handler rx.EventHandler,
	opts ...observable.Option) Connectable {
	ob := observable.CheckEventHandler(handler)
	c.observers = append(c.observers, ob)
	return c
}

// Do is like Subscribe but subscribes a func(interface{}) as a NextHandler
func (c *connector) Do(nextf func(interface{})) Connectable {
	ob := observer.New(handlers.NextFunc(nextf))
	c.observers = append(c.observers, ob)
	return c
}

// Connect activates the Observable stream and returns a channel of Subscription channel.
func (c *connector) Connect() <-chan (chan subscription.Subscription) {
	done := make(chan (chan subscription.Subscription), 1)
	source := []interface{}{}

	for {
		item, err := c.Observable.Next()
		if err != nil {
			break
		}
		source = append(source, item)
	}

	var wg sync.WaitGroup
	wg.Add(len(c.observers))

	for _, ob := range c.observers {
		local := make([]interface{}, len(source))
		copy(local, source)

		fin := make(chan struct{})
		sub := subscription.New().Subscribe()

		go func(ob observer.Observer) {
		OuterLoop:
			for _, item := range local {
				switch item := item.(type) {
				case error:
					ob.OnError(item)

					// Record error
					sub.Error = item
					break OuterLoop
				default:
					ob.OnNext(item)
				}
			}
			fin <- struct{}{}
		}(ob)

		temp := make(chan subscription.Subscription)

		go func(ob observer.Observer) {
			<-fin
			if sub.Error == nil {
				ob.OnDone()
				sub.Unsubscribe()
			}

			go func() {
				temp <- sub
				done <- temp
			}()
			wg.Done()
		}(ob)
	}

	go func() {
		wg.Wait()
		close(done)
	}()

	return done
}

// Map maps a MappableFunc predicate to each item in Connectable and
// returns a new Connectable with applied items.
func (c *connector) Map(fn fx.MappableFunc) Connectable {
	return &connector{
		Observable: c.Observable.Map(fn),
	}
}

// Filter filters items in the original Connectable and returns
// a new Connectable with the filtered items.
func (c *connector) Filter(fn fx.FilterableFunc) Connectable {
	return &connector{
		Observable: c.Observable.Filter(fn),
	}
}

// Scan applies ScannableFunc predicate to each item in the original
// Connectable sequentially and emits each successive value on a new Connectable.
func (c *connector) Scan(apply fx.ScannableFunc) Connectable {
	return &connector{
		Observable: c.Observable.Scan(apply),
	}
}

// First returns new Connectable which emits only first item.
func (c *connector) First() Connectable {
	return &connector{
		Observable: c.Observable.First(),
	}
}

// Last returns a new Connectable which emits only last item.
func (c *connector) Last() Connectable {
	return &connector{
		Observable: c.Observable.Last(),
	}
}

//Distinct suppress duplicate items in the original Connectable and
//returns a new Connectable.
func (c *connector) Distinct(apply fx.KeySelectorFunc) Connectable {
	return &connector{
		Observable: c.Observable.Distinct(apply),
	}
}

//DistinctUntilChanged suppress duplicate items in the original Connectable only
// if they are successive to one another and returns a new Connectable.
func (c *connector) DistinctUntilChanged(apply fx.KeySelectorFunc) Connectable {
	return &connector{
		Observable: c.Observable.DistinctUntilChanged(apply),
	}
}
