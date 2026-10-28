// Porting di test/node-api/test_uv_threadpool_size/test_uv_threadpool_size.c. Il C fa
// #undef NDEBUG, quindi le assert() sono attive anche qui (nt.CAssert). uv_mutex_* diventa
// sync.Mutex e uv_sleep nt.Sleep; le struct del C sono struct Go passate come data
// dell'async work.
package main

import (
	"fmt"
	"os"
	"sync"
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

const (
	// waitingSleepMs e exitSleepMs sono i due uv_sleep di Execute.
	waitingSleepMs = 100
	exitSleepMs    = 1000
)

type asyncSharedData struct {
	mutex              sync.Mutex
	threadpoolSize     uint32
	nTasksStarted      uint32
	nTasksExited       uint32
	nTasksFinalized    uint32
	observedSaturation bool
}

type asyncCarrier struct {
	taskID     uint32
	sharedData *asyncSharedData
	request    sys.AsyncWork
}

func allTasksStarted(d *asyncSharedData) bool {
	nt.CAssert(d.nTasksStarted <= d.threadpoolSize+1, "d->n_tasks_started <= d->threadpool_size + 1")
	return d.nTasksStarted == d.threadpoolSize+1
}

func allTasksExited(d *asyncSharedData) bool {
	nt.CAssert(d.nTasksExited <= d.nTasksStarted, "d->n_tasks_exited <= d->n_tasks_started")
	return allTasksStarted(d) && d.nTasksExited == d.nTasksStarted
}

func allTasksFinalized(d *asyncSharedData) bool {
	nt.CAssert(d.nTasksFinalized <= d.nTasksExited, "d->n_tasks_finalized <= d->n_tasks_exited")
	return allTasksExited(d) && d.nTasksFinalized == d.nTasksExited
}

func stillSaturating(d *asyncSharedData) bool {
	return d.nTasksStarted < d.threadpoolSize
}

func threadpoolSaturated(d *asyncSharedData) bool {
	return d.nTasksStarted == d.threadpoolSize && d.nTasksExited == 0
}

func threadpoolDesaturating(d *asyncSharedData) bool {
	return d.nTasksStarted >= d.threadpoolSize && d.nTasksExited != 0
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// printInfo scrive su stdout come il printf del C: node-options.js verifica che stderr resti vuoto.
func printInfo(label string, c *asyncCarrier) {
	d := c.sharedData
	if _, err := fmt.Fprintf(os.Stdout, "%s task_id=%d n_tasks_started=%d n_tasks_exited=%d "+
		"n_tasks_finalized=%d observed_saturation=%d\n",
		label, c.taskID, d.nTasksStarted, d.nTasksExited, d.nTasksFinalized,
		boolToInt(d.observedSaturation)); err != nil {
		// Non c'è un altro canale: stderr deve restare vuoto.
		return
	}
}

func execute(_ sys.Env, data any) {
	c := data.(*asyncCarrier)
	d := c.sharedData

	// Finché sono partiti meno di threadpool_size lavori ne devono partire altri (prima o poi);
	// solo dopo i lavori in coda devono restare in attesa.
	d.mutex.Lock()
	shouldBeConcurrent := stillSaturating(d)
	d.nTasksStarted++
	nt.CAssert(d.nTasksStarted <= d.threadpoolSize+1, "d->n_tasks_started <= d->threadpool_size + 1")

	printInfo("start", c)

	if shouldBeConcurrent {
		// Attende che il pool sia saturo: poco elegante, ma qui non conta.
		for stillSaturating(d) {
			printInfo("waiting", c)
			d.mutex.Unlock()
			nt.Sleep(waitingSleepMs)
			d.mutex.Lock()
		}

		// Un lavoro vedrà il pool saturo (threadpool_size lavori partiti, nessuno uscito) e sarà
		// il primo a uscire.
		if !d.observedSaturation {
			nt.CAssert(threadpoolSaturated(d), "threadpool_saturated(d)")
			d.observedSaturation = true
		} else {
			nt.CAssert(threadpoolSaturated(d) || threadpoolDesaturating(d),
				"threadpool_saturated(d) || threadpool_desaturating(d)")
		}
	} else {
		// Se il lavoro non è tra i primi threadpool_size, può essere partito solo dopo l'uscita di
		// un altro.
		nt.CAssert(threadpoolDesaturating(d), "threadpool_desaturating(d)")
	}

	printInfo("exit", c)

	// Lascia accedere gli altri lavori ai dati condivisi. Se il pool fosse più grande di
	// threadpool_size partirebbe un lavoro di troppo, e un'asserzione fallirebbe.
	d.mutex.Unlock()
	nt.Sleep(exitSleepMs)
	d.mutex.Lock()
	d.nTasksExited++
	d.mutex.Unlock()
}

func complete(env sys.Env, status sys.Status, data any) {
	c := data.(*asyncCarrier)
	d := c.sharedData

	if status != sys.StatusOK {
		nt.ThrowTypeError(env, "Execute callback failed.")
		return
	}

	d.mutex.Lock()
	nt.CAssert(threadpoolDesaturating(d), "threadpool_desaturating(d)")
	d.nTasksFinalized++
	printInfo("finalize", c)
	if allTasksFinalized(d) {
		// uv_mutex_destroy e free(d): la memoria è del GC di Go.
		d.mutex.Unlock()
	} else {
		d.mutex.Unlock()
	}

	if !nt.Call(env, sys.DeleteAsyncWork(env, c.request)) {
		return
	}
}

func test(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var argv [1]sys.Value
	var this sys.Value
	var data unsafe.Pointer
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &argv[0], &this, &data)) {
		return nil
	}
	if !nt.Assert(env, argc >= 1, "argc >= 1", "Not enough arguments, expected 1.") {
		return nil
	}

	// calloc + uv_mutex_init: una struct Go azzerata con il mutex già pronto.
	sharedData := &asyncSharedData{}

	var t sys.ValueType
	if !nt.Call(env, sys.Typeof(env, argv[0], &t)) {
		return nil
	}
	if !nt.Assert(env, t == sys.TypeNumber, "t == napi_number", "Wrong first argument, integer expected.") {
		return nil
	}
	if !nt.Call(env, sys.GetValueUint32(env, argv[0], &sharedData.threadpoolSize)) {
		return nil
	}

	var resourceName sys.Value
	if !nt.Call(env, sys.CreateStringUTF8(env, nt.CStr("TestResource"), sys.AutoLength, &resourceName)) {
		return nil
	}

	for i := uint32(0); i <= sharedData.threadpoolSize; i++ {
		carrier := &asyncCarrier{taskID: i, sharedData: sharedData}
		if !nt.Call(env, sys.CreateAsyncWork(env, nil, resourceName, sys.AsyncExecuteTrampoline,
			sys.AsyncCompleteTrampoline, nt.AsyncWorkData(execute, complete, carrier), &carrier.request)) {
			return nil
		}
		if !nt.Call(env, sys.QueueAsyncWork(env, carrier.request)) {
			return nil
		}
	}

	return nil
}

func initModule(env sys.Env, exports sys.Value) sys.Value {
	desc := []sys.PropertyDescriptor{nt.DeclareProperty("test", test)}
	if !nt.Call(env, nt.DefineProperties(env, exports, desc)) {
		return nil
	}
	return exports
}
