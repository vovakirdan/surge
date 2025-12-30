## 09 — Prime Sieve (Sieve of Eratosthenes)

Counts the number of primes up to a given limit using the Sieve of Eratosthenes algorithm.

### What it demonstrates
- Sieve of Eratosthenes algorithm
- boolean arrays and initialization
- nested while loops
- numeric literals with underscores
- f-string formatting with variables

### Run
surge run showcases/09_prime_sieve/main.sg

### Similar in other languages

**C:**
```c
#include <stdio.h>
#include <stdbool.h>
#include <stdlib.h>
#include <string.h>

int main() {
    int n = 10000;
    bool *is_prime = (bool*)malloc((n + 1) * sizeof(bool));
    memset(is_prime, true, (n + 1) * sizeof(bool));
    is_prime[0] = false;
    is_prime[1] = false;

    int p = 2;
    while (p * p < n + 1) {
        if (is_prime[p]) {
            int m = p * p;
            while (m < n + 1) {
                is_prime[m] = false;
                m = m + p;
            }
        }
        p = p + 1;
    }

    int count = 0;
    int i = 2;
    while (i < n + 1) {
        if (is_prime[i]) {
            count = count + 1;
        }
        i = i + 1;
    }
    printf("Primes up to %d: %d\n", n, count);
    free(is_prime);
    return 0;
}
```

**Rust:**
```rust
fn main() {
    let n: i32 = 10_000;
    let mut is_prime = vec![true; (n + 1) as usize];
    is_prime[0] = false;
    is_prime[1] = false;

    let mut p = 2;
    while p * p < n + 1 {
        if is_prime[p as usize] {
            let mut m = p * p;
            while m < n + 1 {
                is_prime[m as usize] = false;
                m = m + p;
            }
        }
        p = p + 1;
    }

    let mut count = 0;
    let mut i = 2;
    while i < n + 1 {
        if is_prime[i as usize] {
            count = count + 1;
        }
        i = i + 1;
    }
    println!("Primes up to {}: {}", n, count);
}
```

**Go:**
```go
package main

import "fmt"

func main() {
    n := 10_000
    isPrime := make([]bool, n+1)
    for i := range isPrime {
        isPrime[i] = true
    }
    isPrime[0] = false
    isPrime[1] = false

    p := 2
    for p*p < n+1 {
        if isPrime[p] {
            m := p * p
            for m < n+1 {
                isPrime[m] = false
                m = m + p
            }
        }
        p = p + 1
    }

    count := 0
    i := 2
    for i < n+1 {
        if isPrime[i] {
            count = count + 1
        }
        i = i + 1
    }
    fmt.Printf("Primes up to %d: %d\n", n, count)
}
```

