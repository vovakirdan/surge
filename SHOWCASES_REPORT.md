| Program | VM | LLVM | Notes |
| --- | --- | --- | --- |
| `showcases/01_hello_world` | ok | ok |  |
| `showcases/02_args_echo` | ok | ok |  |
| `showcases/03_stdin_cat` | ok | ok |  |
| `showcases/04_fizzbuzz` | ok | ok |  |
| `showcases/05_collatz` | ok | ok |  |
| `showcases/06_gcd_lcm` | ok | ok |  |
| `showcases/07_linear_search` | ok | ok |  |
| `showcases/08_binary_search` | ok | ok |  |
| `showcases/09_prime_sieve` | ok | ok |  |
| `showcases/10_sort_merge` | ok | ok |  |
| `showcases/11_matrix_mul` | ok | ok |  |
| `showcases/12_histogram` | ok | ok |  |
| `showcases/13_unicode_len_demo` | ok | ok |  |
| `showcases/14_reverse_string` | ok | ok |  |
| `showcases/15_trim_split_join` | ok | ok |  |
| `showcases/16_replace_and_find` | ok | ok |  |
| `showcases/17_bytesview_dump` | ok | ok |  |
| `showcases/18_rope_like_concat` | ok | ok |  |
| `showcases/19_cast_zoo` | ok | ok |  |
| `showcases/20_fixed_overflow` | ok | ok |  |
| `showcases/21_bigint_stress` | ok | ok |  |
| `showcases/22_float_edges` | ok | ok |  |
| `showcases/23_roundtrip_parse_format` | ok | ok |  |
| `showcases/24_option_pipeline` | ok | ok |  |
| `showcases/25_erring_parser` | ok | ok |  |
| `showcases/26_state_machine_tagged` | ok | ok |  |
| `showcases/27_result_aggregation` | ok | ok |  |
| `showcases/28_generic_map_filter` | ok | ok |  |
| `showcases/29_contract_printable` | ok | ok |  |
| `showcases/30_mini_ini_parser` | ok | ok |  |
| `showcases/async/01_spawn_await_order` | ok | fail | LLVM build failed: Error: LLVM emit failed: missing struct info; artifacts: build/showcases/showcases_async_01_spawn_await_order_main_sg |
| `showcases/async/02_fanout_fanin` | ok | fail | LLVM build failed: Error: LLVM emit failed: missing struct info; artifacts: build/showcases/showcases_async_02_fanout_fanin_main_sg |
| `showcases/async/03_channel_prod_cons` | fail | fail | VM run failed (exit 1): no matching overload for producer; LLVM build failed: no matching overload for producer; artifacts: build/showcases/showcases_async_03_channel_prod_cons_main_sg |
| `showcases/async/04_pipeline_3stage` | fail | fail | VM run failed (exit 1): no matching overload for gen; LLVM build failed: no matching overload for gen; artifacts: build/showcases/showcases_async_04_pipeline_3stage_main_sg |
| `showcases/async/05_cancellation` | ok | fail | LLVM build failed: Error: LLVM emit failed: missing struct info; artifacts: build/showcases/showcases_async_05_cancellation_main_sg |
| `showcases/async/06_failfast_scope` | ok | fail | LLVM build failed: Error: LLVM emit failed: missing struct info; artifacts: build/showcases/showcases_async_06_failfast_scope_main_sg |
| `showcases/async/07_checkpoint_scheduler` | ok | fail | LLVM build failed: Error: LLVM emit failed: missing struct info; artifacts: build/showcases/showcases_async_07_checkpoint_scheduler_main_sg |
| `showcases/async/08_timeout_race` | ok | fail | LLVM build failed: Error: LLVM emit failed: missing struct info; artifacts: build/showcases/showcases_async_08_timeout_race_main_sg |
