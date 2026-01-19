# Language Intrinsics

This document lists all the intrinsics available in the Surge language, grouped by their function and type. Intrinsics are built-in functions and types that are implemented directly by the compiler or runtime.

## Functions

### Memory Management

*   `@intrinsic fn rt_alloc(size: uint, align: uint) -> *byte`
    *   Allocates a block of memory with the specified size and alignment.
*   `@intrinsic fn rt_free(ptr: *byte, size: uint, align: uint) -> nothing`
    *   Frees a previously allocated block of memory.
*   `@intrinsic fn rt_realloc(ptr: *byte, old_size: uint, new_size: uint, align: uint) -> *byte`
    *   Reallocates a memory block with a new size.
*   `@intrinsic fn rt_memcpy(dst: *byte, src: *byte, n: uint) -> nothing`
    *   Copies `n` bytes from `src` to `dst`.
*   `@intrinsic fn rt_memmove(dst: *byte, src: *byte, n: uint) -> nothing`
    *   Safely copies `n` bytes from `src` to `dst`, handling overlap.
*   `@intrinsic pub fn rt_heap_stats() -> HeapStats`
    *   Returns current heap statistics (allocations, frees, live blocks, etc.).
*   `@intrinsic pub fn rt_heap_dump() -> string`
    *   Returns a deterministic heap summary string for debugging.

### I/O (Input/Output)

*   `@intrinsic fn rt_write_stdout(ptr: *byte, length: uint) -> uint`
    *   Writes raw bytes to the standard output.
*   `@intrinsic fn rt_write_stderr(ptr: *byte, length: uint) -> uint`
    *   Writes raw bytes to the standard error.
*   `@intrinsic fn rt_read_stdin(buf: *byte, max_len: uint) -> uint`
    *   Reads raw bytes from the standard input.
*   `@intrinsic pub fn readline() -> string`
    *   Reads a single line of text from standard input.
*   `@intrinsic pub fn rt_stdin_read_all() -> string`
    *   Reads all remaining content from standard input.
*   `@intrinsic pub fn rt_argv() -> string[]`
    *   Returns the command-line arguments passed to the program.

### Filesystem

*   `@intrinsic fn rt_fs_cwd() -> Erring<string, FsError>`
    *   Returns the current working directory.
*   `@intrinsic fn rt_fs_metadata(path: &string) -> Erring<Metadata, FsError>`
    *   Returns metadata (size, type, permissions) for a file path.
*   `@intrinsic fn rt_fs_read_dir(path: &string) -> Erring<DirEntry[], FsError>`
    *   Reads the contents of a directory.
*   `@intrinsic fn rt_fs_mkdir(path: &string, recursive: bool) -> Erring<nothing, FsError>`
    *   Creates a new directory (optionally recursive).
*   `@intrinsic fn rt_fs_remove_file(path: &string) -> Erring<nothing, FsError>`
    *   Removes a file.
*   `@intrinsic fn rt_fs_remove_dir(path: &string, recursive: bool) -> Erring<nothing, FsError>`
    *   Removes a directory (optionally recursive).
*   `@intrinsic fn rt_fs_open(path: &string, flags: FsOpenFlags) -> Erring<File, FsError>`
    *   Opens a file with the specified flags.
*   `@intrinsic fn rt_fs_close(file: own File) -> Erring<nothing, FsError>`
    *   Closes an open file.
*   `@intrinsic fn rt_fs_read(file: &File, buf: *byte, cap: uint) -> Erring<uint, FsError>`
    *   Reads from an open file into a buffer.
*   `@intrinsic fn rt_fs_write(file: &File, buf: *byte, length: uint) -> Erring<uint, FsError>`
    *   Writes to an open file from a buffer.
*   `@intrinsic fn rt_fs_seek(file: &File, offset: int, whence: SeekWhence) -> Erring<uint, FsError>`
    *   Changes the read/write position in a file.
*   `@intrinsic fn rt_fs_flush(file: &File) -> Erring<nothing, FsError>`
    *   Flushes any buffered changes to the file.
*   `@intrinsic fn rt_fs_read_file(path: &string) -> Erring<byte[], FsError>`
    *   Convenience function to read an entire file into a byte array.
*   `@intrinsic fn rt_fs_write_file(path: &string, data: *byte, length: uint, flags: FsOpenFlags) -> Erring<nothing, FsError>`
    *   Convenience function to write data to a file.
*   `@intrinsic fn rt_fs_file_name(file: &File) -> Erring<string, FsError>`
    *   Gets the name of an open file.
*   `@intrinsic fn rt_fs_file_type(file: &File) -> Erring<FileType, FsError>`
    *   Gets the type of an open file.
*   `@intrinsic fn rt_fs_file_metadata(file: &File) -> Erring<Metadata, FsError>`
    *   Gets metadata for an open file.

### Network (TCP)

*   `@intrinsic fn rt_net_listen(addr: &string, port: uint) -> NetResult<TcpListener>`
    *   Starts listening for TCP connections on a specified address and port.
*   `@intrinsic fn rt_net_connect(addr: &string, port: uint) -> NetResult<TcpConn>`
    *   Initiates a TCP connection to a remote address and port.
*   `@intrinsic fn rt_net_close_listener(l: own TcpListener) -> NetResult<nothing>`
    *   Closes a TCP listener.
*   `@intrinsic fn rt_net_close_conn(c: own TcpConn) -> NetResult<nothing>`
    *   Closes a TCP connection.
*   `@intrinsic fn rt_net_accept(l: &TcpListener) -> NetResult<TcpConn>`
    *   Accepts a new incoming connection from a listener.
*   `@intrinsic fn rt_net_read(c: &TcpConn, buf: *byte, cap: uint) -> NetResult<uint>`
    *   Reads data from a TCP connection.
*   `@intrinsic fn rt_net_write(c: &TcpConn, buf: *byte, length: uint) -> NetResult<uint>`
    *   Writes data to a TCP connection.
*   `@intrinsic fn rt_net_wait_accept(l: &TcpListener) -> Task<nothing>`
    *   Waits asynchronously until a new connection is available to accept.
*   `@intrinsic fn rt_net_wait_readable(c: &TcpConn) -> Task<nothing>`
    *   Waits asynchronously until the connection is readable.
*   `@intrinsic fn rt_net_wait_writable(c: &TcpConn) -> Task<nothing>`
    *   Waits asynchronously until the connection is writable.

### String Operations

*   `@intrinsic fn rt_string_ptr(s: &string) -> *byte`
    *   Returns a raw pointer to the string's underlying data.
*   `@intrinsic fn rt_string_len(s: &string) -> uint`
    *   Returns the number of Unicode code points in the string.
*   `@intrinsic fn rt_string_len_bytes(s: &string) -> uint`
    *   Returns the length of the string in bytes (UTF-8).
*   `@intrinsic fn rt_string_from_bytes(ptr: *byte, length: uint) -> string`
    *   Creates a string from a raw byte buffer.
*   `@intrinsic fn rt_string_from_utf16(ptr: *uint16, length: uint) -> string`
    *   Creates a string from a UTF-16 buffer.
*   `@intrinsic fn rt_string_index(s: &string, index: int) -> uint32`
    *   Returns the code point at the specified index.
*   `@intrinsic fn rt_string_slice(s: &string, r: Range<int>) -> string`
    *   Returns a substring based on the given range.
*   `@intrinsic fn rt_string_concat(a: &string, b: &string) -> string`
    *   Concatenates two strings.
*   `@intrinsic fn rt_string_eq(a: &string, b: &string) -> bool`
    *   Checks if two strings are equal.
*   `@intrinsic fn rt_string_bytes_view(s: &string) -> BytesView`
    *   Returns a view of the string's bytes.
*   `@intrinsic fn rt_string_force_flatten(s: &string) -> nothing`
    *   Forces a rope/slice string to be flattened into a contiguous buffer (test-only).
*   `extern<string> @intrinsic pub fn from_bytes(bytes: &byte[]) -> Erring<string, Error>`
    *   Creates a string from a byte array.

### Array Operations

*   `@intrinsic fn rt_array_reserve<T>(a: &mut Array<T>, new_cap: uint) -> nothing`
    *   Reserves capacity for the array.
*   `@intrinsic fn rt_array_push<T>(a: &mut Array<T>, value: T) -> nothing`
    *   Appends a value to the end of the array.
*   `@intrinsic fn rt_array_pop<T>(a: &mut Array<T>) -> Option<T>`
    *   Removes and returns the last element of the array.

### Map Operations

*   `@intrinsic fn rt_map_new<K, V>() -> Map<K, V>`
    *   Creates a new empty map.
*   `@intrinsic fn rt_map_len<K, V>(m: &Map<K, V>) -> uint`
    *   Returns the number of elements in the map.
*   `@intrinsic fn rt_map_contains<K, V>(m: &Map<K, V>, key: &K) -> bool`
    *   Checks if the map contains a key.
*   `@intrinsic fn rt_map_get_ref<K, V>(m: &Map<K, V>, key: &K) -> Option<&V>`
    *   Returns a reference to the value associated with the key.
*   `@intrinsic fn rt_map_get_mut<K, V>(m: &mut Map<K, V>, key: &K) -> Option<&mut V>`
    *   Returns a mutable reference to the value associated with the key.
*   `@intrinsic fn rt_map_insert<K, V>(m: &mut Map<K, V>, key: K, value: V) -> Option<V>`
    *   Inserts a key-value pair, returning the old value if present.
*   `@intrinsic fn rt_map_remove<K, V>(m: &mut Map<K, V>, key: &K) -> Option<V>`
    *   Removes a key from the map, returning the value if present.
*   `@intrinsic fn rt_map_keys<K, V>(m: &Map<K, V>) -> K[]`
    *   Returns an array of all keys in the map.

### Ranges

*   `@intrinsic pub fn rt_range_int_new(start: int, end: int, inclusive: bool) -> Range<int>`
    *   Creates a new integer range.
*   `@intrinsic pub fn rt_range_int_from_start(start: int, inclusive: bool) -> Range<int>`
    *   Creates a range starting from a value.
*   `@intrinsic pub fn rt_range_int_to_end(end: int, inclusive: bool) -> Range<int>`
    *   Creates a range ending at a value.
*   `@intrinsic pub fn rt_range_int_full(inclusive: bool) -> Range<int>`
    *   Creates a full range.
*   `extern<Range<T>> @intrinsic pub fn next(self: &mut Range<T>) -> Option<T>`
    *   Advances the range iterator and returns the next value.

### Concurrency (Tasks & Channels)

*   `@intrinsic pub fn rt_worker_count() -> uint`
    *   Returns the number of active async executor workers.
*   `@intrinsic fn rt_scope_enter(failfast: bool) -> uint`
    *   Enters a new async scope.
*   `@intrinsic fn rt_scope_register_child<T>(scope: uint, child: Task<T>) -> nothing`
    *   Registers a child task with the current scope.
*   `@intrinsic fn rt_scope_cancel_all(scope: uint) -> nothing`
    *   Cancels all tasks in the scope.
*   `@intrinsic fn rt_scope_join_all(scope: uint) -> bool`
    *   Waits for all tasks in the scope to complete.
*   `@intrinsic fn rt_scope_exit(scope: uint) -> nothing`
    *   Exits the async scope.
*   `@intrinsic pub fn checkpoint() -> Task<nothing>`
    *   Yields control to the scheduler, allowing other tasks to run.
*   `@intrinsic pub fn sleep(ms: uint) -> Task<nothing>`
    *   Suspends the current task for a specified duration (in milliseconds).
*   `@intrinsic pub fn timeout<T>(t: Task<T>, ms: uint) -> TaskResult<T>`
    *   Runs a task with a timeout.
*   `extern<Task<T>> @intrinsic pub fn clone(self: &Task<T>) -> Task<T>`
    *   Clones a task handle.
*   `extern<Task<T>> @intrinsic pub fn cancel(self: &Task<T>) -> nothing`
    *   Cancels the task.
*   `extern<Task<T>> @intrinsic pub fn await(self: own Task<T>) -> TaskResult<T>`
    *   Awaits the completion of a task.
*   `@intrinsic fn make_channel<T>(capacity: uint) -> own Channel<T>`
    *   Creates a new channel.
*   `extern<Channel<T>> @intrinsic pub fn new(capacity: uint) -> own Channel<T>`
    *   Creates a new channel with the specified capacity.
*   `extern<Channel<T>> @intrinsic pub fn send(self: &Channel<T>, value: own T) -> nothing`
    *   Sends a value to the channel (blocking).
*   `extern<Channel<T>> @intrinsic pub fn recv(self: &Channel<T>) -> Option<T>`
    *   Receives a value from the channel (blocking).
*   `extern<Channel<T>> @intrinsic pub fn try_send(self: &Channel<T>, value: own T) -> bool`
    *   Attempts to send a value (non-blocking).
*   `extern<Channel<T>> @intrinsic pub fn try_recv(self: &Channel<T>) -> Option<T>`
    *   Attempts to receive a value (non-blocking).
*   `extern<Channel<T>> @intrinsic pub fn close(self: &Channel<T>) -> nothing`
    *   Closes the channel.

### Synchronization (Locks & Atomics)

*   `extern<RwLock> @intrinsic pub fn new() -> RwLock`
    *   Creates a new read-write lock.
*   `extern<RwLock> @intrinsic pub fn read_lock(self: &mut RwLock) -> nothing`
    *   Acquires a read lock (blocking).
*   `extern<RwLock> @intrinsic pub fn read_unlock(self: &mut RwLock) -> nothing`
    *   Releases a read lock.
*   `extern<RwLock> @intrinsic pub fn write_lock(self: &mut RwLock) -> nothing`
    *   Acquires a write lock (blocking).
*   `extern<RwLock> @intrinsic pub fn write_unlock(self: &mut RwLock) -> nothing`
    *   Releases a write lock.
*   `extern<RwLock> @intrinsic pub fn try_read_lock(self: &mut RwLock) -> bool`
    *   Attempts to acquire a read lock (non-blocking).
*   `extern<RwLock> @intrinsic pub fn try_write_lock(self: &mut RwLock) -> bool`
    *   Attempts to acquire a write lock (non-blocking).
*   `@intrinsic pub fn atomic_load(ptr: &int) -> int` (overloaded for `uint`, `bool`)
    *   Atomically loads a value.
*   `@intrinsic pub fn atomic_store(ptr: &mut int, value: int) -> nothing` (overloaded for `uint`, `bool`)
    *   Atomically stores a value.
*   `@intrinsic pub fn atomic_exchange(ptr: &mut int, new_val: int) -> int` (overloaded for `uint`, `bool`)
    *   Atomically exchanges a value, returning the old one.
*   `@intrinsic pub fn atomic_compare_exchange(ptr: &mut int, expected: int, desired: int) -> bool` (overloaded for `uint`, `bool`)
    *   Atomically compares and exchanges a value.
*   `@intrinsic pub fn atomic_fetch_add(ptr: &mut int, delta: int) -> int` (overloaded for `uint`)
    *   Atomically adds to a value, returning the old value.
*   `@intrinsic pub fn atomic_fetch_sub(ptr: &mut int, delta: int) -> int` (overloaded for `uint`)
    *   Atomically subtracts from a value, returning the old value.

### Time

*   `@intrinsic pub fn monotonic_now() -> Duration`
    *   Returns the current monotonic time.
*   `extern<Duration> @intrinsic pub fn sub(self: Duration, other: Duration) -> Duration`
    *   Subtracts two durations.
*   `extern<Duration> @intrinsic pub fn as_seconds(self: Duration) -> float`
    *   Returns the duration in seconds.
*   `extern<Duration> @intrinsic pub fn as_millis(self: Duration) -> float`
    *   Returns the duration in milliseconds.
*   `extern<Duration> @intrinsic pub fn as_micros(self: Duration) -> float`
    *   Returns the duration in microseconds.
*   `extern<Duration> @intrinsic pub fn as_nanos(self: Duration) -> float`
    *   Returns the duration in nanoseconds.

### Terminal

*   `@intrinsic pub fn term_enter_alt_screen() -> nothing`
    *   Switches to the alternate terminal screen.
*   `@intrinsic pub fn term_exit_alt_screen() -> nothing`
    *   Exits the alternate terminal screen.
*   `@intrinsic pub fn term_set_raw_mode(enabled: bool) -> nothing`
    *   Enables or disables raw mode.
*   `@intrinsic pub fn term_hide_cursor() -> nothing`
    *   Hides the terminal cursor.
*   `@intrinsic pub fn term_show_cursor() -> nothing`
    *   Shows the terminal cursor.
*   `@intrinsic pub fn term_size() -> (int, int)`
    *   Returns the terminal size (cols, rows).
*   `@intrinsic pub fn term_write(bytes: byte[]) -> nothing`
    *   Writes bytes directly to the terminal.
*   `@intrinsic pub fn term_flush() -> nothing`
    *   Flushes the terminal output buffer.
*   `@intrinsic pub fn term_read_event() -> TermEvent`
    *   Reads an input event from the terminal.

### Miscellaneous & System

*   `@intrinsic fn rt_panic_bounds(kind: uint, index: int, length: int) -> nothing`
    *   Triggers a bounds check panic.
*   `@intrinsic pub fn default<T>() -> T`
    *   Returns the default value for a type.
*   `@intrinsic pub fn size_of<T>() -> uint`
    *   Returns the size of a type in bytes.
*   `@intrinsic pub fn align_of<T>() -> uint`
    *   Returns the alignment of a type in bytes.
*   `@intrinsic pub fn exit<E: ErrorLike>(e: E) -> nothing`
    *   Exits the program with an error.
*   `@intrinsic pub fn rt_panic(ptr: *byte, length: uint) -> nothing`
    *   Triggers a runtime panic with a message.
*   `@intrinsic pub fn rt_exit(code: int) -> nothing`
    *   Exits the process with a specific return code.
*   `@intrinsic pub fn clone<T>(value: &T) -> T`
    *   Creates a deep copy of a value.

### Primitive Operators

*   `__add`, `__sub`, `__mul`, `__div`, `__mod`: Arithmetic operators.
*   `__bit_and`, `__bit_or`, `__bit_xor`, `__shl`, `__shr`: Bitwise operators.
*   `__lt`, `__le`, `__eq`, `__ne`, `__ge`, `__gt`: Comparison operators.
*   `__pos`, `__neg`, `__abs`, `__not`: Unary operators.
*   `__to`: Type conversion operator.
*   `__index`: Indexing operator.
*   `__len`: Length operator.

(Note: These are defined on primitive types `int`, `uint`, `float`, `bool`, `string`, etc. in `core/intrinsics.sg`.)

## Types

### System & Memory

*   `@intrinsic pub type File = { __opaque: int }`
    *   Represents an open file handle.
*   `@intrinsic pub type RwLock = { __opaque: *byte }`
    *   A read-write lock primitive.
*   `@intrinsic pub type Range<T> = { __state: *byte }`
    *   Represents a range of values (iterator).
*   `@intrinsic pub type BytesView = { owner: string, ptr: *byte, len: uint }`
    *   A view into a string's bytes.

### Network

*   `@intrinsic pub type TcpListener = { __opaque: int }`
    *   A TCP socket listener.
*   `@intrinsic pub type TcpConn = { __opaque: int }`
    *   A TCP connection.

### Concurrency

*   `@intrinsic pub type Task<T> = { __opaque: int }`
    *   A handle to an asynchronous task.
*   `@intrinsic pub type Channel<T> = { __opaque: *byte }`
    *   A channel for communication between tasks.

### Time

*   `@intrinsic pub type Duration = { __opaque: int64 }`
    *   Represents a span of time with nanosecond precision.
