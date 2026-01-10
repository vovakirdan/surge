package vm_test

import (
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func mtThreadCount(t *testing.T) int {
	t.Helper()
	threads := runtime.NumCPU()
	if threads > 4 {
		threads = 4
	}
	if threads < 2 {
		t.Skip("MT correctness tests require >=2 CPUs")
	}
	return threads
}

func mtEnv(t *testing.T) []string {
	t.Helper()
	root := repoRoot(t)
	threads := mtThreadCount(t)
	baseEnv := envWithStdlib(root)
	return overrideEnv(baseEnv, strconv.Itoa(threads))
}

func runMTSource(t *testing.T, source string, timeout time.Duration) {
	t.Helper()
	outputPath := buildLLVMProgramFromSource(t, source)
	env := mtEnv(t)
	dur, res := runBinaryWithTimeout(t, outputPath, env, timeout)
	if res.exitCode != 0 {
		t.Fatalf("run failed (exit=%d, dur=%s)\nstdout:\n%s\nstderr:\n%s",
			res.exitCode, dur, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "ok") {
		t.Fatalf("unexpected stdout: %q", res.stdout)
	}
}

func TestMTCorrectnessWakeups(t *testing.T) {
	ensureLLVMToolchain(t)
	t.Parallel()

	source := `async fn stress_sender(id: int, rounds: int, gate: own Channel<int>) -> int {
    let mut i = 0;
    while i < rounds {
        checkpoint().await();
        gate.send(id);
        i = i + 1;
    }
    return id;
}

async fn drain(gate: own Channel<int>, total: int) -> int {
    let mut seen = 0;
    while seen < total {
        let v = gate.recv();
        let ok = compare v {
            Some(_) => true;
            nothing => false;
        };
        if !ok {
            return 1;
        }
        seen = seen + 1;
    }
    return 0;
}

async fn recv_once(ch: own Channel<int>) -> int {
    let v = ch.recv();
    let ok = compare v {
        Some(_) => true;
        nothing => false;
    };
    if !ok {
        return 1;
    }
    return 0;
}

async fn send_once(ch: own Channel<int>, value: int) -> int {
    ch.send(value);
    return 0;
}

async fn send_after(ch: own Channel<int>, value: int) -> int {
    checkpoint().await();
    checkpoint().await();
    ch.send(value);
    return 0;
}

async fn main_async() -> int {
    if rt_worker_count() <= 1:uint {
        return 90;
    }

    let workers = 8;
    let rounds = 500;
    let total = workers * rounds;
    let gate = make_channel::<int>(0);

    let sink_task = spawn drain(gate, total);
    let mut tasks: Task<int>[] = Array::<Task<int>>::with_len(workers to uint);
    let mut i = 0;
    while i < workers {
        let c = gate;
        tasks[i] = spawn stress_sender(i, rounds, c);
        i = i + 1;
    }

    let mut cancelled = false;
    for t in tasks {
        let r = t.await();
        let ok = compare r {
            Success(_) => true;
            Cancelled() => false;
        };
        if !ok {
            cancelled = true;
        }
    }
    if cancelled {
        return 1;
    }
    let sink_res = sink_task.await();
    let sink_ok = compare sink_res {
        Success(v) => v == 0;
        Cancelled() => false;
    };
    if !sink_ok {
        return 2;
    }

    let rounds_race = 500;
    let mut j = 0;
    while j < rounds_race {
        let ch = make_channel::<int>(0);
        let recv_task = spawn recv_once(ch);
        let send_task = spawn send_once(ch, j);
        let r1 = recv_task.await();
        let r2 = send_task.await();
        let ok1 = compare r1 {
            Success(v) => v == 0;
            Cancelled() => false;
        };
        let ok2 = compare r2 {
            Success(v) => v == 0;
            Cancelled() => false;
        };
        if !ok1 || !ok2 {
            return 4;
        }
        j = j + 1;
    }

    let mut k = 0;
    while k < rounds_race {
        let ch = make_channel::<int>(0);
        let recv_task = spawn recv_once(ch);
        let send_task = spawn send_after(ch, k);
        let r1 = recv_task.await();
        let r2 = send_task.await();
        let ok1 = compare r1 {
            Success(v) => v == 0;
            Cancelled() => false;
        };
        let ok2 = compare r2 {
            Success(v) => v == 0;
            Cancelled() => false;
        };
        if !ok1 || !ok2 {
            return 5;
        }
        k = k + 1;
    }

    print("ok");
    return 0;
}

@entrypoint
fn main() -> int {
    let r = main_async().await();
    let code = compare r {
        Success(v) => v;
        Cancelled() => 99;
    };
    return code;
}
`

	outputPath := buildLLVMProgramFromSource(t, source)
	env := mtEnv(t)
	env = overrideEnvVar(env, "SURGE_BLOCKING_THREADS", "1")
	dur, res := runBinaryWithTimeout(t, outputPath, env, 20*time.Second)
	if res.exitCode != 0 {
		t.Fatalf("run failed (exit=%d, dur=%s)\nstdout:\n%s\nstderr:\n%s",
			res.exitCode, dur, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "ok") {
		t.Fatalf("unexpected stdout: %q", res.stdout)
	}
}

func TestMTCorrectnessChannels(t *testing.T) {
	ensureLLVMToolchain(t)
	t.Parallel()

	source := `@copy
type CountSum = { count: int, sum: int };

async fn producer(ch: own Channel<int>, base: int, count: int) -> int {
    let mut i = 0;
    while i < count {
        ch.send(base + i);
        i = i + 1;
    }
    return count;
}

async fn consumer(ch: own Channel<int>) -> CountSum {
    let mut count = 0;
    let mut sum = 0;
    let mut done = false;
    while !done {
        let v = ch.recv();
        compare v {
            Some(x) => {
                count = count + 1;
                sum = sum + x;
                nothing;
            }
            nothing => {
                done = true;
                nothing;
            }
        };
    }
    return { count = count, sum = sum };
}

async fn wait_recv(ch: own Channel<int>) -> int {
    let v = ch.recv();
    let ok = compare v {
        Some(_) => true;
        nothing => false;
    };
    if ok {
        return 0;
    }
    return 1;
}

async fn wait_send(ch: own Channel<int>) -> int {
    ch.send(1);
    return 0;
}

async fn sem_worker(sem: Semaphore, done: own Channel<int>, id: int) -> int {
    let mut s = sem;
    s.acquire().await();
    checkpoint().await();
    done.send(id);
    s.release();
    return id;
}

async fn cond_waiter(cond: Condition, mtx: Mutex, done: own Channel<int>, id: int) -> int {
    let m = mtx;
    let lock_task = m.lock();
    lock_task.await();
    let wait_task = cond.wait(&m);
    wait_task.await();
    m.unlock();
    done.send(id);
    return id;
}

async fn barrier_worker(barrier: Barrier, done: own Channel<int>, id: int) -> int {
    barrier.arrive_and_wait().await();
    done.send(id);
    return id;
}

async fn barrier_worker_twice(barrier: Barrier, done: own Channel<int>, id: int) -> int {
    barrier.arrive_and_wait().await();
    barrier.arrive_and_wait().await();
    done.send(id);
    return id;
}

async fn barrier_cancel_test(barrier: Barrier) -> int {
    let bdone = make_channel::<int>(1);
    let bt = spawn barrier_worker(barrier, bdone, 1);
    let cancel_probe = timeout(bt.clone(), 50:uint);
    let probe_ok = compare cancel_probe {
        Success(_) => false;
        Cancelled() => true;
    };
    if !probe_ok {
        return 24;
    }
    let br = bt.await();
    let cancel_ok = compare br {
        Success(_) => false;
        Cancelled() => true;
    };
    if !cancel_ok {
        return 25;
    }
    return 0;
}

async fn recv_int(ch: own Channel<int>) -> Option<int> {
    return ch.recv();
}

async fn main_async() -> int {
    if rt_worker_count() <= 1:uint {
        return 90;
    }

    let producers = 4;
    let consumers = 4;
    let per = 500;
    let total = producers * per;
    let ch = make_channel::<int>(0);

    let mut prod_tasks: Task<int>[] = Array::<Task<int>>::with_len(producers to uint);
    let mut cons_tasks: Task<CountSum>[] = Array::<Task<CountSum>>::with_len(consumers to uint);

    let mut i = 0;
    while i < producers {
        let c = ch;
        prod_tasks[i] = spawn producer(c, i * per, per);
        i = i + 1;
    }

    let mut j = 0;
    while j < consumers {
        let c = ch;
        cons_tasks[j] = spawn consumer(c);
        j = j + 1;
    }

    let mut prod_cancelled = false;
    for task in prod_tasks {
        let r = task.await();
        let ok = compare r {
            Success(_) => true;
            Cancelled() => false;
        };
        if !ok {
            prod_cancelled = true;
        }
    }
    if prod_cancelled {
        return 1;
    }

    ch.close();

    let mut total_count = 0;
    let mut total_sum = 0;
    let mut cons_cancelled = false;
    for task in cons_tasks {
        let r = task.await();
        let ok = compare r {
            Success(v) => {
                total_count = total_count + v.count;
                total_sum = total_sum + v.sum;
                true;
            }
            Cancelled() => false;
        };
        if !ok {
            cons_cancelled = true;
        }
    }
    if cons_cancelled {
        return 2;
    }
    if total_count != total {
        return 3;
    }
    let expected = (total - 1) * total / 2;
    if total_sum != expected {
        return 4;
    }

    let recv_ch = make_channel::<int>(0);
    let recv_task = spawn wait_recv(recv_ch);
    checkpoint().await();
    checkpoint().await();
    recv_task.cancel();
    let recv_res = recv_task.await();
    let recv_cancel_ok = compare recv_res {
        Success(_) => false;
        Cancelled() => true;
    };
    if !recv_cancel_ok {
        return 5;
    }

    let send_ch = make_channel::<int>(0);
    let send_task = spawn wait_send(send_ch);
    checkpoint().await();
    checkpoint().await();
    send_task.cancel();
    let send_res = send_task.await();
    let send_cancel_ok = compare send_res {
        Success(_) => false;
        Cancelled() => true;
    };
    if !send_cancel_ok {
        return 6;
    }

    let sem = Semaphore.new(2:uint);
    let done = make_channel::<int>(8);
    let sem_total: int = 6;
    let mut sem_tasks: Task<int>[] = Array::<Task<int>>::with_len(sem_total to uint);
    let mut sidx: int = 0;
    while sidx < sem_total {
        let d = done;
        sem_tasks[sidx] = spawn sem_worker(sem, d, sidx);
        sidx = sidx + 1;
    }
    let mut sem_seen: int = 0;
    let mut sem_sum: int = 0;
    while sem_seen < sem_total {
        let v_res = recv_int(done).await();
        let v = compare v_res {
            Success(opt) => opt;
            Cancelled() => nothing;
        };
        compare v {
            Some(id) => {
                sem_sum = sem_sum + id;
                sem_seen = sem_seen + 1;
                nothing;
            }
            nothing => {
                return 7;
            }
        };
    }
    let sem_expected: int = (sem_total - 1) * sem_total / 2;
    if sem_sum != sem_expected {
        return 8;
    }
    let mut sem_cancelled = false;
    for t in sem_tasks {
        let r = t.await();
        let ok = compare r {
            Success(_) => true;
            Cancelled() => false;
        };
        if !ok {
            sem_cancelled = true;
        }
    }
    if sem_cancelled {
        return 9;
    }

    let mut sem0 = Semaphore.new(0:uint);
    let done0 = make_channel::<int>(2);
    let d0 = done0;
    let t1 = spawn sem_worker(sem0, d0, 1);
    checkpoint().await();
    checkpoint().await();
    t1.cancel();
    let r1 = t1.await();
    let cancel_ok = compare r1 {
        Success(_) => false;
        Cancelled() => true;
    };
    if !cancel_ok {
        return 10;
    }
    let d1 = done0;
    let t2 = spawn sem_worker(sem0, d1, 2);
    sem0.release();
    let r2 = t2.await();
    let ok2 = compare r2 {
        Success(_) => true;
        Cancelled() => false;
    };
    if !ok2 {
        return 11;
    }
    let v2_res = recv_int(done0).await();
    let v2 = compare v2_res {
        Success(opt) => opt;
        Cancelled() => nothing;
    };
    let got2 = compare v2 {
        Some(id) => id == 2;
        nothing => false;
    };
    if !got2 {
        return 12;
    }

    let cond1 = Condition.new();
    let mtx1 = Mutex.new();
    let cdone1 = make_channel::<int>(1);
    let cd1 = cdone1;
    let ct1 = spawn cond_waiter(cond1, mtx1, cd1, 1);
    checkpoint().await();
    checkpoint().await();
    cond1.notify_one();
    let cv1_res = recv_int(cdone1).await();
    let cv1 = compare cv1_res {
        Success(opt) => opt;
        Cancelled() => nothing;
    };
    let okc1 = compare cv1 {
        Some(id) => id == 1;
        nothing => false;
    };
    if !okc1 {
        return 13;
    }
    let cr1 = ct1.await();
    let okr1 = compare cr1 {
        Success(_) => true;
        Cancelled() => false;
    };
    if !okr1 {
        return 14;
    }

    let cond2 = Condition.new();
    let mtx2 = Mutex.new();
    let cdone2 = make_channel::<int>(4);
    let mut ctasks: Task<int>[] = Array::<Task<int>>::with_len(3:uint);
    let mut cidx: int = 0;
    while cidx < 3 {
        let cd2 = cdone2;
        ctasks[cidx] = spawn cond_waiter(cond2, mtx2, cd2, cidx);
        cidx = cidx + 1;
    }
    checkpoint().await();
    checkpoint().await();
    cond2.notify_all();
    let mut got = 0;
    while got < 3 {
        let v_res = recv_int(cdone2).await();
        let v = compare v_res {
            Success(opt) => opt;
            Cancelled() => nothing;
        };
        let ok = compare v {
            Some(_) => true;
            nothing => false;
        };
        if !ok {
            return 15;
        }
        got = got + 1;
    }
    let mut cond_cancelled = false;
    for t in ctasks {
        let r = t.await();
        let ok = compare r {
            Success(_) => true;
            Cancelled() => false;
        };
        if !ok {
            cond_cancelled = true;
        }
    }
    if cond_cancelled {
        return 16;
    }

    let cond3 = Condition.new();
    let mtx3 = Mutex.new();
    let cdone3 = make_channel::<int>(1);
    let cd3 = cdone3;
    let ct2 = spawn cond_waiter(cond3, mtx3, cd3, 1);
    checkpoint().await();
    checkpoint().await();
    ct2.cancel();
    let cr2 = ct2.await();
    let cancel_ok2 = compare cr2 {
        Success(_) => false;
        Cancelled() => true;
    };
    if !cancel_ok2 {
        return 17;
    }
    let cd4 = cdone3;
    let ct3 = spawn cond_waiter(cond3, mtx3, cd4, 2);
    cond3.notify_one();
    let cv3_res = recv_int(cdone3).await();
    let cv3 = compare cv3_res {
        Success(opt) => opt;
        Cancelled() => nothing;
    };
    let okc3 = compare cv3 {
        Some(id) => id == 2;
        nothing => false;
    };
    if !okc3 {
        return 18;
    }
    let cr3 = ct3.await();
    let okr3 = compare cr3 {
        Success(_) => true;
        Cancelled() => false;
    };
    if !okr3 {
        return 19;
    }

    let barrier1 = Barrier.new(4:uint);
    let bdone1 = make_channel::<int>(4);
    let mut btasks: Task<int>[] = Array::<Task<int>>::with_len(4:uint);
    let mut b = 0;
    while b < 4 {
        let bd1 = bdone1;
        btasks[b] = spawn barrier_worker(barrier1, bd1, b);
        b = b + 1;
    }
    let mut bgot = 0;
    while bgot < 4 {
        let v_res = recv_int(bdone1).await();
        let v = compare v_res {
            Success(opt) => opt;
            Cancelled() => nothing;
        };
        let ok = compare v {
            Some(_) => true;
            nothing => false;
        };
        if !ok {
            return 20;
        }
        bgot = bgot + 1;
    }
    let mut barr_cancelled = false;
    for t in btasks {
        let r = t.await();
        let ok = compare r {
            Success(_) => true;
            Cancelled() => false;
        };
        if !ok {
            barr_cancelled = true;
        }
    }
    if barr_cancelled {
        return 21;
    }

    let barrier2 = Barrier.new(3:uint);
    let bdone2 = make_channel::<int>(3);
    let mut btasks2: Task<int>[] = Array::<Task<int>>::with_len(3:uint);
    let mut k = 0;
    while k < 3 {
        let bd2 = bdone2;
        btasks2[k] = spawn barrier_worker_twice(barrier2, bd2, k);
        k = k + 1;
    }
    let mut bgot2 = 0;
    while bgot2 < 3 {
        let v_res = recv_int(bdone2).await();
        let v = compare v_res {
            Success(opt) => opt;
            Cancelled() => nothing;
        };
        let ok = compare v {
            Some(_) => true;
            nothing => false;
        };
        if !ok {
            return 22;
        }
        bgot2 = bgot2 + 1;
    }
    let mut barr_cancelled2 = false;
    for t in btasks2 {
        let r = t.await();
        let ok = compare r {
            Success(_) => true;
            Cancelled() => false;
        };
        if !ok {
            barr_cancelled2 = true;
        }
    }
    if barr_cancelled2 {
        return 23;
    }

    let barrier3 = Barrier.new(2:uint);
    let cancel_task = barrier_cancel_test(barrier3);
    let cancel_res = cancel_task.await();
    let cancel_code = compare cancel_res {
        Success(v) => v;
        Cancelled() => 26;
    };
    if cancel_code != 0 {
        return cancel_code;
    }

    print("ok");
    return 0;
}

@entrypoint
fn main() -> int {
    let r = main_async().await();
    let code = compare r {
        Success(v) => v;
        Cancelled() => 99;
    };
    return code;
}
`

	runMTSource(t, source, 20*time.Second)
}

func TestMTStructuredConcurrency(t *testing.T) {
	ensureLLVMToolchain(t)
	t.Parallel()

	source := `async fn spin(count: int) -> int {
    let mut i = 0;
    while i < count {
        checkpoint().await();
        i = i + 1;
    }
    return count;
}

async fn send_done(done: own Channel<int>, id: int, spins: int) -> int {
    let _ = spin(spins).await();
    done.send(id);
    return id;
}

async fn worker_recv(ch: own Channel<int>) -> int {
    checkpoint().await();
    ch.recv();
    return 1;
}

async fn join_worker(t: Task<int>) -> int {
    let r = t.await();
    let ok = compare r {
        Success(_) => true;
        Cancelled() => false;
    };
    if ok {
        return 0;
    }
    return 1;
}

async fn main_async() -> int {
    if rt_worker_count() <= 1:uint {
        return 90;
    }

    let pres = (async {
        let done = make_channel::<int>(8);
        let mut i = 0;
        while i < 4 {
            let c = done;
            let t = spawn send_done(c, i, 10 + i);
            let _ = t;
            i = i + 1;
        }
        return done;
    }).await();
    let mut done_ch = make_channel::<int>(0);
    let parent_ok = compare pres {
        Success(v) => {
            done_ch = v;
            true;
        }
        Cancelled() => false;
    };
    if !parent_ok {
        return 1;
    }
    let mut got = 0;
    while got < 4 {
        let v = done_ch.try_recv();
        let ok = compare v {
            Some(_) => true;
            nothing => false;
        };
        if !ok {
            return 2;
        }
        got = got + 1;
    }

    let ff = (@failfast async {
        let slow = spawn async {
            let _ = spin(200).await();
            return 1;
        };
        let fast = spawn async {
            checkpoint().await();
            return 2;
        };
        fast.cancel();
        let r_fast = fast.await();
        let fast_cancelled = compare r_fast {
            Cancelled() => true;
            Success(_) => false;
        };
        if !fast_cancelled {
            return 10;
        }
        let r_slow = slow.await();
        let slow_cancelled = compare r_slow {
            Cancelled() => true;
            Success(_) => false;
        };
        if !slow_cancelled {
            return 11;
        }
        return 0;
    }).await();
    let ff_ok = compare ff {
        Cancelled() => true;
        Success(_) => false;
    };
    if !ff_ok {
        return 12;
    }

    let ff2 = (@failfast async {
        let a = spawn async {
            let _ = spin(50).await();
            return 1;
        };
        let b = spawn async {
            let _ = spin(50).await();
            return 2;
        };
        a.cancel();
        b.cancel();
        let _ = a.await();
        let _ = b.await();
        return 0;
    }).await();
    let ff2_ok = compare ff2 {
        Cancelled() => true;
        Success(_) => false;
    };
    if !ff2_ok {
        return 13;
    }

    let long = spawn spin(200);
    let long_clone = long.clone();
    let r_timeout = timeout(long_clone, 5);
    let timed_out = compare r_timeout {
        Cancelled() => true;
        Success(_) => false;
    };
    if !timed_out {
        return 20;
    }
    let r_long = long.await();
    let long_cancelled = compare r_long {
        Cancelled() => true;
        Success(_) => false;
    };
    if !long_cancelled {
        return 21;
    }

    let short = spawn spin(3);
    let short_clone = short.clone();
    let r_short = timeout(short_clone, 200);
    let short_ok = compare r_short {
        Success(_) => true;
        Cancelled() => false;
    };
    if !short_ok {
        return 22;
    }
    let r_short2 = short.await();
    let short2_ok = compare r_short2 {
        Success(_) => true;
        Cancelled() => false;
    };
    if !short2_ok {
        return 23;
    }

    let join_res = async {
        let ch = make_channel::<int>(0);
        let worker = spawn worker_recv(ch);
        let worker_clone = worker.clone();
        let joiner = spawn join_worker(worker_clone);
        checkpoint().await();
        joiner.cancel();
        let jr = joiner.await();
        let jr_ok = compare jr {
            Cancelled() => true;
            Success(_) => false;
        };
        if !jr_ok {
            return 30;
        }
        worker.cancel();
        let _ = worker.await();
        return 0;
    }.await();
    let join_ok = compare join_res {
        Success(v) => v == 0;
        Cancelled() => false;
    };
    if !join_ok {
        return 31;
    }

    print("ok");
    return 0;
}

@entrypoint
fn main() -> int {
    let r = main_async().await();
    let code = compare r {
        Success(v) => v;
        Cancelled() => 99;
    };
    return code;
}
`

	runMTSource(t, source, 20*time.Second)
}

func TestMTBlockingPool(t *testing.T) {
	ensureLLVMToolchain(t)
	t.Parallel()

	source := `fn busy_loop(iter: int) -> int {
    let mut i = 0;
    let mut acc = 0;
    while i < iter {
        acc = acc + (i % 2);
        i = i + 1;
    }
    return acc;
}

async fn progress_worker(steps: int) -> int {
    let mut i = 0;
    while i < steps {
        checkpoint().await();
        i = i + 1;
    }
    return steps;
}

async fn cancel_waiter(started: own Channel<int>, spin_iters: int) -> int {
    let b = blocking {
        return busy_loop(spin_iters);
    };
    started.send(1);
    let _ = b.await();
    return 0;
}

async fn main_async() -> int {
    if rt_worker_count() <= 1:uint {
        return 90;
    }
    let workers: int = rt_worker_count() to int;
    let spin_iters: int = 50000;
    let mut blocking_count = workers;
    if blocking_count > 2 {
        blocking_count = 2;
    }

    let mut blocking_tasks: Task<int>[] = Array::<Task<int>>::with_len(blocking_count to uint);
    let mut i = 0;
    while i < blocking_count {
        let iters = spin_iters;
        blocking_tasks[i] = blocking {
            return busy_loop(iters);
        };
        i = i + 1;
    }
    let mut y = 0;
    while y < blocking_count {
        checkpoint().await();
        y = y + 1;
    }

    let mut progress_tasks: Task<int>[] = Array::<Task<int>>::with_len(workers to uint);
    i = 0;
    while i < workers {
        progress_tasks[i] = spawn progress_worker(200);
        i = i + 1;
    }
    let mut progress_cancelled = false;
    for t in progress_tasks {
        let r = t.await();
        let ok = compare r {
            Success(_) => true;
            Cancelled() => false;
        };
        if !ok {
            progress_cancelled = true;
        }
    }
    if progress_cancelled {
        return 1;
    }
    for b in blocking_tasks {
        let r = b.await();
        let ok = compare r {
            Success(_) => true;
            Cancelled() => false;
        };
        if !ok {
            return 2;
        }
    }

    let cancel_iters = spin_iters / 2;
    let blocker = blocking {
        return busy_loop(spin_iters);
    };
    let started = make_channel::<int>(1);
    let st = started;
    let waiter = spawn cancel_waiter(st, cancel_iters);
    let s = started.recv();
    let s_ok = compare s {
        Some(_) => true;
        nothing => false;
    };
    if !s_ok {
        return 10;
    }
    checkpoint().await();
    waiter.cancel();
    let wres = waiter.await();
    let w_ok = compare wres {
        Cancelled() => true;
        Success(_) => false;
    };
    if !w_ok {
        return 11;
    }
    let bres = blocker.await();
    let b_ok = compare bres {
        Success(_) => true;
        Cancelled() => true;
    };
    if !b_ok {
        return 12;
    }

    let stress_jobs: int = blocking_count * 2;
    let stress_tasks: int = workers * 2;
    let mut stress_blocking: Task<int>[] = Array::<Task<int>>::with_len(stress_jobs to uint);
    let mut j = 0;
    while j < stress_jobs {
        let iters = cancel_iters;
        stress_blocking[j] = blocking {
            return busy_loop(iters);
        };
        j = j + 1;
    }
    let mut k = 0;
    while k < workers {
        checkpoint().await();
        k = k + 1;
    }

    let mut stress_tasks_arr: Task<int>[] = Array::<Task<int>>::with_len(stress_tasks to uint);
    j = 0;
    while j < stress_tasks {
        stress_tasks_arr[j] = spawn progress_worker(100);
        j = j + 1;
    }
    let mut stress_cancelled = false;
    for t in stress_tasks_arr {
        let r = t.await();
        let ok = compare r {
            Success(_) => true;
            Cancelled() => false;
        };
        if !ok {
            stress_cancelled = true;
        }
    }
    if stress_cancelled {
        return 20;
    }
    for b in stress_blocking {
        let r = b.await();
        let ok = compare r {
            Success(_) => true;
            Cancelled() => false;
        };
        if !ok {
            return 21;
        }
    }

    print("ok");
    return 0;
}

@entrypoint
fn main() -> int {
    let r = main_async().await();
    let code = compare r {
        Success(v) => v;
        Cancelled() => 99;
    };
    return code;
}
`

	runMTSource(t, source, 20*time.Second)
}
