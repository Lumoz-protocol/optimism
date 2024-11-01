package main

import (
	"fmt"
	"os"
	"reflect"
	"runtime"
	"sync"
)

func main() {
	TestCondSignal()
	TestCondSignalGenerations()
	TestCondBroadcast()
	TestRace()
	TestCondSignalStealing()
	TestCondCopy()

	fmt.Println("Cond test passed")
	runtime.GC()
	_, _ = os.Stdout.Write([]byte("GC complete!\n"))
}

func TestCondSignal() {
	var m sync.Mutex
	c := sync.NewCond(&m)
	n := 2
	running := make(chan bool, n)
	awake := make(chan bool, n)
	for i := 0; i < n; i++ {
		go func() {
			m.Lock()
			running <- true
			c.Wait()
			awake <- true
			m.Unlock()
		}()
	}
	for i := 0; i < n; i++ {
		<-running // Wait for everyone to run.
	}
	for n > 0 {
		select {
		case <-awake:
			_, _ = fmt.Fprintln(os.Stderr, "goroutine not asleep")
			os.Exit(1)
		default:
		}
		m.Lock()
		c.Signal()
		m.Unlock()
		<-awake // Will deadlock if no goroutine wakes up
		select {
		case <-awake:
			_, _ = fmt.Fprintln(os.Stderr, "too many goroutines awake")
			os.Exit(1)
		default:
		}
		n--
	}
	c.Signal()
}

func TestCondSignalGenerations() {
	var m sync.Mutex
	c := sync.NewCond(&m)
	n := 100
	running := make(chan bool, n)
	awake := make(chan int, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			m.Lock()
			running <- true
			c.Wait()
			awake <- i
			m.Unlock()
		}(i)
		if i > 0 {
			a := <-awake
			if a != i-1 {
				_, _ = fmt.Fprintf(os.Stderr, "wrong goroutine woke up: want %d, got %d\n", i-1, a)
				os.Exit(1)
			}
		}
		<-running
		m.Lock()
		c.Signal()
		m.Unlock()
	}
}

func TestCondBroadcast() {
	var m sync.Mutex
	c := sync.NewCond(&m)
	n := 200
	running := make(chan int, n)
	awake := make(chan int, n)
	exit := false
	for i := 0; i < n; i++ {
		go func(g int) {
			m.Lock()
			for !exit {
				running <- g
				c.Wait()
				awake <- g
			}
			m.Unlock()
		}(i)
	}
	for i := 0; i < n; i++ {
		for i := 0; i < n; i++ {
			<-running // Will deadlock unless n are running.
		}
		if i == n-1 {
			m.Lock()
			exit = true
			m.Unlock()
		}
		select {
		case <-awake:
			_, _ = fmt.Fprintln(os.Stderr, "goroutine not asleep")
			os.Exit(1)
		default:
		}
		m.Lock()
		c.Broadcast()
		m.Unlock()
		seen := make([]bool, n)
		for i := 0; i < n; i++ {
			g := <-awake
			if seen[g] {
				_, _ = fmt.Fprintln(os.Stderr, "goroutine woke up twice")
				os.Exit(1)
			}
			seen[g] = true
		}
	}
	select {
	case <-running:
		_, _ = fmt.Fprintln(os.Stderr, "goroutine still running")
		os.Exit(1)
	default:
	}
	c.Broadcast()
}

func TestRace() {
	x := 0
	c := sync.NewCond(&sync.Mutex{})
	done := make(chan bool)
	go func() {
		c.L.Lock()
		x = 1
		c.Wait()
		if x != 2 {
			_, _ = fmt.Fprintln(os.Stderr, "want 2")
			os.Exit(1)
		}
		x = 3
		c.Signal()
		c.L.Unlock()
		done <- true
	}()
	go func() {
		c.L.Lock()
		for {
			if x == 1 {
				x = 2
				c.Signal()
				break
			}
			c.L.Unlock()
			runtime.Gosched()
			c.L.Lock()
		}
		c.L.Unlock()
		done <- true
	}()
	go func() {
		c.L.Lock()
		for {
			if x == 2 {
				c.Wait()
				if x != 3 {
					_, _ = fmt.Fprintln(os.Stderr, "want 3")
					os.Exit(1)
				}
				break
			}
			if x == 3 {
				break
			}
			c.L.Unlock()
			runtime.Gosched()
			c.L.Lock()
		}
		c.L.Unlock()
		done <- true
	}()
	<-done
	<-done
	<-done
}

func TestCondSignalStealing() {
	for iters := 0; iters < 1000; iters++ {
		var m sync.Mutex
		cond := sync.NewCond(&m)

		// Start a waiter.
		ch := make(chan struct{})
		go func() {
			m.Lock()
			ch <- struct{}{}
			cond.Wait()
			m.Unlock()

			ch <- struct{}{}
		}()

		<-ch
		m.Lock()
		m.Unlock()

		// We know that the waiter is in the cond.Wait() call because we
		// synchronized with it, then acquired/released the mutex it was
		// holding when we synchronized.
		//
		// Start two goroutines that will race: one will broadcast on
		// the cond var, the other will wait on it.
		//
		// The new waiter may or may not get notified, but the first one
		// has to be notified.
		done := false
		go func() {
			cond.Broadcast()
		}()

		go func() {
			m.Lock()
			for !done {
				cond.Wait()
			}
			m.Unlock()
		}()

		// Check that the first waiter does get signaled.
		<-ch

		// Release the second waiter in case it didn't get the
		// broadcast.
		m.Lock()
		done = true
		m.Unlock()
		cond.Broadcast()
	}
}

func TestCondCopy() {
	defer func() {
		err := recover()
		if err == nil || err.(string) != "sync.Cond is copied" {
			_, _ = fmt.Fprintf(os.Stderr, "got %v, expect sync.Cond is copied", err)
			os.Exit(1)
		}
	}()
	c := sync.Cond{L: &sync.Mutex{}}
	c.Signal()
	var c2 sync.Cond
	reflect.ValueOf(&c2).Elem().Set(reflect.ValueOf(&c).Elem()) // c2 := c, hidden from vet
	c2.Signal()
}
