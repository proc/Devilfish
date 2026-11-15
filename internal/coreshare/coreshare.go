// Package coreshare lets several engine processes on one machine divide the
// CPU cores between them instead of each starting a full set of search
// threads.
//
// The processes meet in a directory of lock files. A process that starts a
// search holds one "active" lock for as long as it searches, and one "slot"
// lock per core it uses. Every lock is an advisory file lock, which the
// operating system drops when the process exits or dies, so nothing has to
// be cleaned up. An idle process holds nothing.
package coreshare

import (
	"os"
	"path/filepath"
	"sync"
)

// Mode says what kind of search is asking for cores.
type Mode int

const (
	// Search is a search the engine has to answer: it gets the fair share,
	// the cores divided by the number of processes searching right now.
	Search Mode = iota
	// Ponder is a "go ponder" search, which a ponderhit turns into a Search
	// without restarting it. It gets the thread count a Search would, but
	// does not count as searching until Promote.
	Ponder
	// Idle is a search nobody is waiting for (auto-ponder). It takes only
	// the cores no other process has claimed, and one thread if there are
	// none.
	Idle
)

// maxActive bounds the number of processes that can be counted as searching
// at the same time; more than that are not counted.
const maxActive = 64

// EnvDir names the environment variable that overrides the directory the
// processes meet in. Processes with different directories do not see each
// other.
const EnvDir = "DEVILFISH_CORES_DIR"

// DefaultDir is the directory used when EnvDir is not set:
// ~/.devilfish/cores, or a directory under the system's temporary directory
// when there is no home directory.
func DefaultDir() string {
	if d := os.Getenv(EnvDir); d != "" {
		return d
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".devilfish", "cores")
	}
	return filepath.Join(os.TempDir(), "devilfish-cores")
}

// Pool is one process's view of the shared cores. Its methods may be called
// from any goroutine.
type Pool struct {
	dir   string
	cores int

	mu     sync.Mutex
	claim  uint64 // identifies the current claim; 0 means none
	last   uint64
	mode   Mode
	active lockFile   // the active lock held, if any
	slots  []lockFile // the slot locks held
}

// Open returns a pool of the given number of cores that meets other
// processes in dir. The directory is created when it is first needed.
func Open(dir string, cores int) *Pool {
	if cores < 1 {
		cores = 1
	}
	return &Pool{dir: dir, cores: cores}
}

// Cores is the number of cores the pool divides.
func (p *Pool) Cores() int { return p.cores }

// Acquire replaces the pool's current claim with one for a new search and
// returns the number of threads the search should use, between 1 and
// maxThreads, with a value that identifies the claim for Release.
//
// When the lock files cannot be used (no permission, an unsupported
// platform), the search gets maxThreads, as it would without sharing.
func (p *Pool) Acquire(maxThreads int, mode Mode) (threads int, claim uint64) {
	if maxThreads < 1 {
		maxThreads = 1
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.releaseLocked()
	p.last++
	p.claim, p.mode = p.last, mode

	if err := os.MkdirAll(p.dir, 0o755); err != nil {
		return maxThreads, p.claim
	}
	// The directory lock keeps two processes from counting and claiming at
	// the same moment. It is held for a few file operations only.
	guard, ok := lockWait(filepath.Join(p.dir, "lock"))
	if !ok {
		return maxThreads, p.claim
	}
	defer guard.unlock()

	switch mode {
	case Idle:
		p.takeSlots(maxThreads)
		threads = max(1, len(p.slots))
	default:
		others := p.countActive()
		threads = min(maxThreads, max(1, p.cores/(others+1)))
		if mode == Search {
			p.takeActive()
		}
		// Another process that started alone may still hold more than its
		// share until its search ends; the slots it holds are then missing
		// here, and the thread count stands regardless.
		p.takeSlots(threads)
	}
	return threads, p.claim
}

// Promote turns the current Ponder claim into a Search claim (ponderhit):
// from now on the process counts as searching.
func (p *Pool) Promote() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.claim == 0 || p.mode != Ponder {
		return
	}
	p.mode = Search
	guard, ok := lockWait(filepath.Join(p.dir, "lock"))
	if !ok {
		return
	}
	defer guard.unlock()
	p.takeActive()
}

// Release gives up the claim Acquire returned. It does nothing when a later
// Acquire has already replaced that claim.
func (p *Pool) Release(claim uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if claim != 0 && claim == p.claim {
		p.releaseLocked()
	}
}

func (p *Pool) releaseLocked() {
	p.active.unlock()
	p.active = lockFile{}
	for _, s := range p.slots {
		s.unlock()
	}
	p.slots = p.slots[:0]
	p.claim = 0
}

// countActive returns the number of other processes searching now.
func (p *Pool) countActive() int {
	n := 0
	for i := 0; i < maxActive; i++ {
		if isLocked(p.activePath(i)) {
			n++
		}
	}
	return n
}

func (p *Pool) takeActive() {
	for i := 0; i < maxActive; i++ {
		if l, ok := lockTry(p.activePath(i)); ok {
			p.active = l
			return
		}
	}
}

// takeSlots claims up to n of the free core slots.
func (p *Pool) takeSlots(n int) {
	for i := 0; i < p.cores && len(p.slots) < n; i++ {
		if l, ok := lockTry(filepath.Join(p.dir, "slot-"+itoa(i))); ok {
			p.slots = append(p.slots, l)
		}
	}
}

func (p *Pool) activePath(i int) string {
	return filepath.Join(p.dir, "active-"+itoa(i))
}

func itoa(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return itoa(i/10) + string(rune('0'+i%10))
}
