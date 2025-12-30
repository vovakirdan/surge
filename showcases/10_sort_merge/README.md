## 10 — Merge Sort

Sorts an array using the merge sort algorithm with a recursive divide-and-conquer approach.

### What it demonstrates
- merge sort algorithm (recursive)
- working with mutable array references
- helper functions for merging and sorting
- array-to-string conversion
- divide-and-conquer pattern

### Run
surge run showcases/10_sort_merge/main.sg

### Similar in other languages

**C:**
```c
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

void merge(int a[], int tmp[], int lo, int mid, int hi) {
    int i = lo;
    int j = mid;
    int k = lo;

    while (i < mid && j < hi) {
        if (a[i] <= a[j]) {
            tmp[k] = a[i];
            i = i + 1;
        } else {
            tmp[k] = a[j];
            j = j + 1;
        }
        k = k + 1;
    }
    while (i < mid) {
        tmp[k] = a[i];
        i = i + 1;
        k = k + 1;
    }
    while (j < hi) {
        tmp[k] = a[j];
        j = j + 1;
        k = k + 1;
    }

    for (int t = lo; t < hi; t = t + 1) {
        a[t] = tmp[t];
    }
}

void merge_sort(int a[], int tmp[], int lo, int hi) {
    if (hi - lo <= 1) {
        return;
    }
    int mid = lo + (hi - lo) / 2;
    merge_sort(a, tmp, lo, mid);
    merge_sort(a, tmp, mid, hi);
    merge(a, tmp, lo, mid, hi);
}

void arr_to_str(int a[], int len, char* s) {
    strcpy(s, "[");
    for (int i = 0; i < len; i = i + 1) {
        char num[32];
        sprintf(num, "%d", a[i]);
        strcat(s, num);
        if (i < len - 1) {
            strcat(s, ", ");
        }
    }
    strcat(s, "]");
}

int main() {
    int a[] = {9, 1, 8, 2, 7, 3, 6, 4, 5};
    int len = 9;
    int tmp[9];

    printf("Before:\n");
    char buf[256];
    arr_to_str(a, len, buf);
    printf("%s\n", buf);

    merge_sort(a, tmp, 0, len);

    printf("After:\n");
    arr_to_str(a, len, buf);
    printf("%s\n", buf);
    return 0;
}
```

**Rust:**
```rust
fn merge(a: &mut [i32], tmp: &mut [i32], lo: usize, mid: usize, hi: usize) {
    let mut i = lo;
    let mut j = mid;
    let mut k = lo;

    while i < mid && j < hi {
        if a[i] <= a[j] {
            tmp[k] = a[i];
            i = i + 1;
        } else {
            tmp[k] = a[j];
            j = j + 1;
        }
        k = k + 1;
    }
    while i < mid {
        tmp[k] = a[i];
        i = i + 1;
        k = k + 1;
    }
    while j < hi {
        tmp[k] = a[j];
        j = j + 1;
        k = k + 1;
    }

    for t in lo..hi {
        a[t] = tmp[t];
    }
}

fn merge_sort(a: &mut [i32], tmp: &mut [i32], lo: usize, hi: usize) {
    if hi - lo <= 1 {
        return;
    }
    let mid = lo + (hi - lo) / 2;
    merge_sort(a, tmp, lo, mid);
    merge_sort(a, tmp, mid, hi);
    merge(a, tmp, lo, mid, hi);
}

fn arr_to_str(a: &[i32]) -> String {
    let mut s = String::from("[");
    for (i, &v) in a.iter().enumerate() {
        s.push_str(&v.to_string());
        if i < a.len() - 1 {
            s.push_str(", ");
        }
    }
    s.push(']');
    s
}

fn main() {
    let mut a = vec![9, 1, 8, 2, 7, 3, 6, 4, 5];
    let mut tmp = vec![0; a.len()];

    println!("Before:");
    println!("{}", arr_to_str(&a));
    merge_sort(&mut a, &mut tmp, 0, a.len());
    println!("After:");
    println!("{}", arr_to_str(&a));
}
```

**Go:**
```go
package main

import (
    "fmt"
    "strings"
)

func merge(a []int, tmp []int, lo, mid, hi int) {
    i := lo
    j := mid
    k := lo

    for i < mid && j < hi {
        if a[i] <= a[j] {
            tmp[k] = a[i]
            i = i + 1
        } else {
            tmp[k] = a[j]
            j = j + 1
        }
        k = k + 1
    }
    for i < mid {
        tmp[k] = a[i]
        i = i + 1
        k = k + 1
    }
    for j < hi {
        tmp[k] = a[j]
        j = j + 1
        k = k + 1
    }

    for t := lo; t < hi; t = t + 1 {
        a[t] = tmp[t]
    }
}

func mergeSort(a []int, tmp []int, lo, hi int) {
    if hi-lo <= 1 {
        return
    }
    mid := lo + (hi-lo)/2
    mergeSort(a, tmp, lo, mid)
    mergeSort(a, tmp, mid, hi)
    merge(a, tmp, lo, mid, hi)
}

func arrToStr(a []int) string {
    var parts []string
    for _, v := range a {
        parts = append(parts, fmt.Sprintf("%d", v))
    }
    return "[" + strings.Join(parts, ", ") + "]"
}

func main() {
    a := []int{9, 1, 8, 2, 7, 3, 6, 4, 5}
    tmp := make([]int, len(a))

    fmt.Println("Before:")
    fmt.Println(arrToStr(a))
    mergeSort(a, tmp, 0, len(a))
    fmt.Println("After:")
    fmt.Println(arrToStr(a))
}
```

