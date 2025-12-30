## 06 — GCD & LCM

Computes `gcd(a, b)` and `lcm(a, b)`.

### What it demonstrates
- defining helper functions
- loops + modulo
- basic math utilities (like `abs`)
- clean string formatting patterns

### Run
surge run showcases/06_gcd_lcm/main.sg

### Similar in other languages

**C:**
```c
#include <stdio.h>
#include <stdlib.h>

int abs_int(int x) {
    if (x < 0) {
        return -x;
    }
    return x;
}

int gcd(int a, int b) {
    a = abs_int(a);
    b = abs_int(b);
    while (b != 0) {
        int temp = a % b;
        a = b;
        b = temp;
    }
    return a;
}

int lcm(int a, int b) {
    if (a == 0 || b == 0) {
        return 0;
    }
    return abs_int(a / gcd(a, b) * b);
}

int main() {
    int a = 48;
    int b = 18;
    int g = gcd(a, b);
    int l = lcm(a, b);
    printf("a = %d\n", a);
    printf("b = %d\n", b);
    printf("gcd = %d\n", g);
    printf("lcm = %d\n", l);
    return 0;
}
```

**Rust:**
```rust
fn abs_int(x: i64) -> i64 {
    if x < 0 {
        -x
    } else {
        x
    }
}

fn gcd(mut a: i64, mut b: i64) -> i64 {
    a = abs_int(a);
    b = abs_int(b);
    while b != 0 {
        let temp = a % b;
        a = b;
        b = temp;
    }
    a
}

fn lcm(a: i64, b: i64) -> i64 {
    if a == 0 || b == 0 {
        return 0;
    }
    abs_int(a / gcd(a, b) * b)
}

fn main() {
    let a = 48;
    let b = 18;
    let g = gcd(a, b);
    let l = lcm(a, b);
    println!("a = {}", a);
    println!("b = {}", b);
    println!("gcd = {}", g);
    println!("lcm = {}", l);
}
```

**Go:**
```go
package main

import "fmt"

func absInt(x int) int {
    if x < 0 {
        return -x
    }
    return x
}

func gcd(a, b int) int {
    a = absInt(a)
    b = absInt(b)
    for b != 0 {
        temp := a % b
        a = b
        b = temp
    }
    return a
}

func lcm(a, b int) int {
    if a == 0 || b == 0 {
        return 0
    }
    return absInt(a / gcd(a, b) * b)
}

func main() {
    a := 48
    b := 18
    g := gcd(a, b)
    l := lcm(a, b)
    fmt.Printf("a = %d\n", a)
    fmt.Printf("b = %d\n", b)
    fmt.Printf("gcd = %d\n", g)
    fmt.Printf("lcm = %d\n", l)
}
```
