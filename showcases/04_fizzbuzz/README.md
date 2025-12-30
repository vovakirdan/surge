## 04 — FizzBuzz

Classic control-flow demo.

### What it demonstrates
- `for` loop with ranges
- boolean expressions
- modulo arithmetic
- `if / else if / else`

### Run
surge run showcases/04_fizzbuzz/main.sg

### Similar in other languages

**C:**
```c
#include <stdio.h>

int main() {
    for (int i = 1; i <= 100; i = i + 1) {
        int fizz = (i % 3 == 0);
        int buzz = (i % 5 == 0);
        if (fizz && buzz) {
            printf("FizzBuzz\n");
        } else if (fizz) {
            printf("Fizz\n");
        } else if (buzz) {
            printf("Buzz\n");
        } else {
            printf("%d\n", i);
        }
    }
    return 0;
}
```

**Rust:**
```rust
fn main() {
    for i in 1..=100 {
        let fizz = i % 3 == 0;
        let buzz = i % 5 == 0;
        if fizz && buzz {
            println!("FizzBuzz");
        } else if fizz {
            println!("Fizz");
        } else if buzz {
            println!("Buzz");
        } else {
            println!("{}", i);
        }
    }
}
```

**Go:**
```go
package main

import "fmt"

func main() {
    for i := 1; i <= 100; i = i + 1 {
        fizz := i%3 == 0
        buzz := i%5 == 0
        if fizz && buzz {
            fmt.Println("FizzBuzz")
        } else if fizz {
            fmt.Println("Fizz")
        } else if buzz {
            fmt.Println("Buzz")
        } else {
            fmt.Println(i)
        }
    }
}
```
