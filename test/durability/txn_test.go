package durability

import (
	"bufio"
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/sky-valley/pagelike/internal/store"
)

// TestInterruptedMultiDocumentTransaction kills a writer in the middle of a
// transaction that spans several documents and their events, then reopens
// the store: none of the transaction's documents or events may be visible,
// and a later complete transaction must commit all of them.
func TestInterruptedMultiDocumentTransaction(t *testing.T) {
	helper := filepath.Join(t.TempDir(), "crashwriter")
	if out, err := exec.Command("go", "build", "-o", helper, "./crashwriter").CombinedOutput(); err != nil {
		t.Fatalf("build helper: %v %s", err, out)
	}
	dir := t.TempDir()
	cmd := exec.Command(helper, dir, "5")
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), "wrote 2") { // three documents written, not committed
			cmd.Process.Signal(syscall.SIGKILL)
			break
		}
	}
	cmd.Wait()

	st, err := store.Open(dir)
	if err != nil {
		t.Fatalf("reopen after crash: %v", err)
	}
	docs, _ := st.List(context.Background(), "/", false)
	_, maxSeq, _ := st.EventBounds(context.Background())
	st.Close()
	if len(docs) != 0 || maxSeq != 0 {
		t.Fatalf("interrupted transaction left %d documents and events up to %d visible", len(docs), maxSeq)
	}

	if out, err := exec.Command(helper, dir, "5").CombinedOutput(); err != nil || !strings.Contains(string(out), "committed") {
		t.Fatalf("second run: %v %s", err, out)
	}
	st, _ = store.Open(dir)
	defer st.Close()
	docs, _ = st.List(context.Background(), "/", false)
	evs, _ := st.EventsAfter(context.Background(), "/doc-4.html", 0)
	if len(docs) != 5 || len(evs) != 1 {
		t.Fatalf("complete transaction: %d documents, %d events for doc-4", len(docs), len(evs))
	}
}
