// Command crashwriter writes several documents and events in ONE store
// transaction, pausing between writes so a test can SIGKILL it mid-transaction.
package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/sky-valley/pagelike/internal/store"
)

func main() {
	st, err := store.Open(os.Args[1])
	if err != nil {
		panic(err)
	}
	n, _ := strconv.Atoi(os.Args[2])
	_, err = st.Update(context.Background(), func(tx *store.Tx) error {
		for i := 0; i < n; i++ {
			if _, err := tx.Put(&store.Document{Path: fmt.Sprintf("/doc-%d.html", i), ContentType: "text/html", Body: []byte(fmt.Sprintf("<p>%d</p>", i))}); err != nil {
				return err
			}
			if err := tx.AddEvent(store.Event{Path: fmt.Sprintf("/doc-%d.html", i), Name: "mutation", Data: "x"}); err != nil {
				return err
			}
			fmt.Println("wrote", i)
			time.Sleep(200 * time.Millisecond)
		}
		return nil
	})
	if err != nil {
		panic(err)
	}
	fmt.Println("committed")
}
