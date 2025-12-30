## 11 — Matrix Multiplication

Multiplies two N×N matrices (represented as flat arrays) and computes a checksum of the result.

### What it demonstrates
- matrix multiplication algorithm
- working with 2D data as flat arrays (indexing: `i * N + j`)
- nested for loops (triple nested)
- array initialization with default values
- checksum computation

### Run
surge run showcases/11_matrix_mul/main.sg

### Similar in other languages

**C:**
```c
#include <stdio.h>

#define N 16

int main() {
    int a[N * N];
    int b[N * N];
    int c[N * N];

    // init arrays
    for (int i = 0; i < N; i = i + 1) {
        for (int j = 0; j < N; j = j + 1) {
            a[i * N + j] = i + j;
            b[i * N + j] = i + j;
        }
    }

    // multiply
    for (int i = 0; i < N; i = i + 1) {
        for (int j = 0; j < N; j = j + 1) {
            int sum = 0;
            for (int k = 0; k < N; k = k + 1) {
                sum = sum + a[i * N + k] * b[k * N + j];
            }
            c[i * N + j] = sum;
        }
    }

    // print result
    int chk = 0;
    for (int t = 0; t < N * N; t = t + 1) {
        chk = chk + c[t];
    }
    printf("Checksum: %d\n", chk);
    return 0;
}
```

**Rust:**
```rust
const N: usize = 16;

fn main() {
    let mut a = vec![0; N * N];
    let mut b = vec![0; N * N];
    let mut c = vec![0; N * N];

    // init arrays
    for i in 0..N {
        for j in 0..N {
            a[i * N + j] = (i + j) as i32;
            b[i * N + j] = (i + j) as i32;
        }
    }

    // multiply
    for i in 0..N {
        for j in 0..N {
            let mut sum = 0;
            for k in 0..N {
                sum = sum + a[i * N + k] * b[k * N + j];
            }
            c[i * N + j] = sum;
        }
    }

    // print result
    let mut chk = 0;
    for t in 0..(N * N) {
        chk = chk + c[t];
    }
    println!("Checksum: {}", chk);
}
```

**Go:**
```go
package main

import "fmt"

const N = 16

func main() {
    a := make([]int, N*N)
    b := make([]int, N*N)
    c := make([]int, N*N)

    // init arrays
    for i := 0; i < N; i = i + 1 {
        for j := 0; j < N; j = j + 1 {
            a[i*N+j] = i + j
            b[i*N+j] = i + j
        }
    }

    // multiply
    for i := 0; i < N; i = i + 1 {
        for j := 0; j < N; j = j + 1 {
            sum := 0
            for k := 0; k < N; k = k + 1 {
                sum = sum + a[i*N+k]*b[k*N+j]
            }
            c[i*N+j] = sum
        }
    }

    // print result
    chk := 0
    for t := 0; t < N*N; t = t + 1 {
        chk = chk + c[t]
    }
    fmt.Printf("Checksum: %d\n", chk)
}
```

