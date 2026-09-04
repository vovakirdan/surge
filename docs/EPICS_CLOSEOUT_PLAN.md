# Закрытие оставшихся Runtime V2 эпиков

## Что из этого плана выполнено (04.09.2026)

**Этапы 1–3 закрыты. Этап 4 (Epic 22 Phase 2) остаётся следующей работой** —
он и по исходному замыслу шёл после 23b.

| Этап | Состояние | Чем закрыт |
|---|---|---|
| **1 — Wave D** | **Закрыт** `625926c4` | Freeze-набор на `43ae205a` с судьёй на `2b849208`: кампания 1000 повторов зелёная после того, как судья стал утверждать СВОЙСТВО («блок отвечает Cancelled ровно тогда, когда так ответил хотя бы один член»), а не расписание; W8 дважды; рейт 312 — 200 из 200; DEBT-312 закрыт замером |
| **2 — Wave E** | **Закрыт** | Три переписи, которыми владела волна (`native-payload-bits`, `native-word-carrier`, `numeric-drop-dispatch`), читают живой ноль; насыщение паркует производителя без busy-retry (`anchored-saturation-parks-the-producer-and-a-freed-slot-wakes-it`, parks=1 wakes=1, с Rule-13 контролем). Байтовые кредиты в состав НЕ вошли и не отложены: ruling 29.08 установил, что указательный транспорт не берёт плату за байты, бюджет — слоты |
| **3 — Wave F и Epic 21 Task 9** | **Закрыт** `ea50ca0b` / `b46bbebb` | Парный бенч зелёный: 46 рядов из 46, 37 538 попыток, `strict`, все статусы passed; W8 20/20 дважды (1039 и 1047 с); sanitizer (13 valgrind-рядов, `-race`, ASan/UBSan, TSan); golden ×2 и детерминизм корпуса; матрица Task 9 при 1, 2 и 8 шардах под memcheck; перепись 0 в wave-owned категориях; Sentrux (все правила, 5438→5448 рантайм, 6146→6167 корень); пять линз ревью. DEBT-125 и DEBT-126 закрыты |
| **4 — Epic 22 Phase 2** | **Не начат** | Следующая работа: `int`/`uint` reclamation |

**Статусы после закрытия:** Epic 21 и Epic 23b читаются COMPLETE в
`21-owner-routed-frees.md`, `README.md`, `PLAN.md` и
`23b-inline-storage-and-typed-carriers.md`. Ветка слита fast-forward в
`codex/runtime-net-scheduler-refactor` (`90ce4976`).

**Дефекты, найденные и закрытые по дороге:** RV2-DEBT-331 (отменённое тело
не освобождало anchored-состояние — `state_type_id` шёл нулём), RV2-DEBT-332
(send-плечо far select слало payload опаковым словом), обе — находки матрицы
Task 9; у обеих теперь есть именованный мутант.

**Что осталось открытым и почему это не блокирует:** RV2-DEBT-061 (редкий
invalid free на пути immediate-`on`, предсуществующий), 318 (пути миграции VM
— по ruling 03.09 это эпик представления VM, не эта волна), 323 и 325–328
(семейство shutdown), 333 (релизная сборка компилирует рантайм и IR без `-O`
— решение об уровне оптимизации за владельцем, с перемером обеих сторон).
Ни один не является условием выхода волны.

**Чего в плане не было и что пришлось решить по ходу** — девять решений
владельца по протоколу парного бенча за один день, от порога p95 до правила
«батч короче миллисекунды отвечает бюджету 0.90». Причина оказалась не в
коде: скорость фикстуры — свойство физических страниц её файла, шесть
побайтно одинаковых копий читаются от 37 до 50 мкс. Плюс снятие двух
liveness-проб, которые не могли пройти никогда: их точка синхронизации не
армирована нигде, а запись, которую ждёт парсер, не печатает никто.

## Текущее положение и цель

| Эпик | Сейчас | Условие закрытия |
|---|---|---|
| Epic 23b | Wave D почти реализована, но новый liveness blocker делает текущий Nightly красным; Wave E/F открыты | Закрыть Waves D, E и F со всеми нормативными доказательствами |
| Epic 21 | Реализация готова, открыт Task 9 / DEBT-125 | Закрыть acceptance внутри Wave F |
| Epic 22 | Phases 0a/0b и локальный `float` готовы; crossings и `int`/`uint` не завершены | После 23b закончить crossing barriers и Phase 2 |
| Остальные эпики | Закрыты | Финальный live-census подтверждает отсутствие новых обязательных хвостов |

Текущий d2956347 считается interrupted/red: Ryzen-прогон завис и focused-тест воспроизвёл зависание. Сервер сейчас
очищен и простаивает; финальная нагрузочная кампания ещё не запускалась.

Результат работы: Epic 23b, Epic 21 и Epic 22 честно получают COMPLETE; обязательные owned debts закрыты
доказательствами. Неблокирующие баги остаются в DEBT с владельцем и условием возврата.

## Зафиксированные интерфейсы и решения

- Пользовательский язык не получает нового синтаксиса или UX.
- Совместимость с Runtime V1 и временными V2-путями не сохраняется: старые carriers, adapters и boxing удаляются вместе
  с новым путём.

- Channel использует Close-wins по owner-lane order:
    - Commit → Close сохраняет доставленное значение;
    - Close → Commit отклоняет commit, будит receiver как closed и уничтожает payload ровно один раз;
    - detached claim остаётся owner-visible до terminal retirement.

- Локальный spawn, захвативший borrow, получает выводимую compiler-ом carrier affinity:
    - internal task metadata содержит единственного eligible carrier;
    - такая задача не steal’ится, не handoff’ится и завершается на исходном carrier;
    - crossing и blocking с borrow отвергаются;
    - никакого нового @local-подобного атрибута не вводится;
    - **уточнено 2026-09-02:** affinity необходима и НЕ достаточна. Заимствованное место продвигается в
      address-stable фиксированное поле stable task activation storage родителя, а публикация и пробуждение
      affine-задачи адресуются её классу пригодности, а не группе;
    - **уточнено 2026-09-02:** `@entrypoint` — не исключение из модели, а synthetic root task на initial carrier;
      поверхность языка не меняется.

- Far task/channel/select переходят с result_bits/out_bits на exact typed storage с ValueOps и явной ownership
  obligation.

- Transport сохраняет независимые slot budgets DATA=64, CONTROL=16; reply capacity резервируется при admission.
  Произвольный typed reply использует заранее зарезервированный DATA resource, а не CONTROL.

- Epic 22 использует non-atomic RC. Локальные копии heap-backed int/uint делают retain; copied crossings делают deep
  clone; эксклюзивный own move может передать единственное владение без sharing.

- DEBT-125 закрывается внутри Wave F.
- Performance contract Epic 22: 95% throughput / 110% p95 только для сопоставимых fast paths; heap-bignum закрывается
  абсолютными ownership/resource gates.

## Этап 1 — закрыть Wave D

1. Сохранить контекст и закрепить authority.
    - В начале implementation-фазы записать в agentmemory три подтверждённых owner-решения, Close-wins, seq=0 invariant
      и точные данные Ryzen-инцидента.

    - Работать от чистого integration worktree, не трогая dirty canonical checkout.
    - Использовать codebase-memory graph-first для поиска, но проверять найденный код в фактическом integration
      worktree.

    - Старые dirty worktree Scope/277 переносить по проверенным hunks, не cherry-pick’ать целиком и не переносить
      преждевременные записи Closed.

2. Починить надёжность тестового harness.
    - Запускать дочерний C harness в отдельной process group.
    - На timeout завершать всю группу: bounded SIGTERM, затем SIGKILL.
    - Добавить regression test, доказывающий отсутствие orphan-процессов после timeout.

3. Исправить новый seq=0 liveness blocker.
    - Deterministic stand фиксирует окно: первая регистрация → spurious wake → повторный park → две seq=0 registrations
      → deferred cleanup → terminal reply.

    - Общий primitive запрещает deferred removal удалять retry registrations, судьбу которых решает terminal wake-all.
    - Не распространять правило автоматически на timer/by-id keys без отдельной lifecycle-проверки.
    - Positive требует один request/body/reply, правильный select arm, DONE и пустой waiter store.
    - Rule-13 mutant возвращает старый sweep и обязан получить terminal pending + WAITING caller + ноль registrations.
    - Включить stand в обязательный aggregate и CI.

4. Реализовать DEBT-307 одной вертикалью; DEBT-303 закрывается её седьмым шагом.

    **Owner ruling 2026-09-02.** Прежняя формулировка — «доказать affinity, удалить emitAsyncRefParamBox, передать
    настоящий pointer» — отозвана как недостаточная. Обычное локальное место в LLVM это alloca на одну итерацию poll:
    suspension пакует живые значения в task state, а следующий poll распаковывает их в НОВЫЕ alloca, поэтому ребёнок,
    удержавший исходный указатель, повисает после suspend родителя даже на том же carrier. Общий carrier необходим и
    недостаточен. Два независимых ревью нашли в сохранённой частичной реализации два P0 — общий group wake credit для
    affine-публикации и не-path-sensitive `UnpinForTask`; оба входят в эту же вертикаль, а сама частичная реализация
    не коммитится.

    Порядок шагов обязателен: до готовности шагов 2–6 старый gate обязан продолжать отвергать unsafe borrow path.
    Промежуточное состояние «sema разрешила borrow, scheduler прикрепил задачу, но указатель всё ещё alloca»
    запрещено — оно объявляет доказательство, которого нет.

    1. Нормативно закрыть исключение хранения и семантику root-entrypoint carrier (сделано этой полосой):
       узкое исключение §7 storage model для carrier-affine borrow в promoted address-stable место; понятие
       stable task activation storage вместо привязки к символу `__AsyncState$`; `@entrypoint` исполняется как
       synthetic root task рантайма на initial carrier, `fn main()` остаётся `fn main()`.

    2. Сделать pin-состояние borrow path-sensitive в sema: релиз только при definite completion на ВСЕХ достижимых
       входящих путях, решётка may-be-live `ACTIVE + RELEASED -> ACTIVE`, через тот же snapshot/merge, что и
       ownership flow. Move-состояние handle и pin-состояние referent — разные факты.

    3. Добавить анализ «места, требующие stable promotion»: place-oriented, из capture set, только те места, чей
       адрес попадает в захват задачи.

    4. Реализовать fixed-offset promoted storage для async- и root-активаций: резиденты исключены из resume
       pack/unpack, ровно один владелец, drop через обычное обязательство места.

    5. Только после этого — lowering настоящей borrow-ссылки вместо бокса.

    6. Carrier-addressed publication и targeted wake для affine-задач. Нового правила не вводится: §10 RUNTIME_V2
       уже требует адресовать публикацию и уведомление классу пригодности. Steal-refusal после dequeue остаётся
       защитой, а не механизмом корректности.

    7. Удалить emitAsyncRefParamBox вместе с временным путём боксинга; этим закрывается DEBT-303.

    8. Только теперь — приёмка: 2/4/8 carriers, yield/requeue, cancellation, never-polled child, scope shutdown,
       independent non-borrowing child, steal-refusal mutants, borrow-capturing spawn из синхронного
       `@entrypoint`, address-stability места через suspend родителя, join на части путей не снимает pin,
       и strict-zero для core/sync.

5. Интегрировать Scope provenance / SEM3209.
    - Перенести существующую реализацию поверх нового task ABI.
    - creation_scope_key устанавливается один раз до publication и сохраняется через alias/helper/destructure/branch.
    - Scope cancellation не переусыновляет задачу и не меняет creation provenance.
    - Сохранить уже одобренный UX SEM3209 и deterministic 2/4/8 tests.

6. Исправить и интегрировать DEBT-277.
    - Перенести ветку поверх общего seq=0 primitive.
    - Retry budget остаётся 8.
    - Использовать bounded pending-prefix array максимум на семь предыдущих отказов {channel, operation,
      refusal_cause}.

    - При первом exhaustion зарегистрировать текущий arm, проверить generation/state и flush’нуть все предыдущие
      регистрации под соответствующими owner lanes; последующие отказы обрабатывать сразу.

    - Не вводить heap allocation, epoch, новый lifecycle bit или unbounded rescan.
    - Матрица: send7/send8, send7/recv8, recv7/send8, busy/busy→release, ранний exhausted arm + поздний ready winner,
      exact cleanup/drop/pin.

    - Получить новый независимый APPROVE; до этого DEBT остаётся open.

7. Перепроверить Close-wins channel protocol.
    - Удаление receiver из FIFO атомарно переводит его в owner-visible claim registry.
    - Проверить reserve→close→commit, reserve→commit→close, reserve→close→abort, cancel и shutdown.
    - Во всех случаях один wake, один drop/move и один pin/claim retirement.

8. Заморозить единственный финальный Wave D SHA.
    - После последней scheduler/carrier правки выполнить полный §12 gate set, behaviour MT/all, topology 1×1, 1×8, 8×8,
      carrier census, sanitizer/Valgrind, C/static/file-size gates, два golden-прогона, Sentrux и независимый review.

    - Полный tagged internal/vm suite должен пройти на новом SHA; d2956347 не является доказательством.
    - Затем на полностью свободном Ryzen выполнить ровно 1000 non-vacuous повторов:

    ```bash
    SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 \
    taskset -c 8-15 go test ./internal/vm \
      -run '^TestRuntimeV2(FailfastJoinAnswersCancelled|TimeoutTargetAnswersCancelledToEveryHandle)$' \
      -count=1 -parallel=1 -p=1 -v --timeout 300s
    ```

    - Записать SHA, checkout, host identity, CPU affinity, команду, wall time, pass/fail/vacuous counts и все различные
      signatures.

    - Исторические 200+600, разные SHA и красный Nightly не суммировать.
    - После кампании повторить полный gate set; только затем закрыть Wave D и DEBT-312.

## Этап 2 — Wave E: exact carriers и bounded transport

1. Перед изменением файлов, уже превышающих 500 строк, вынести устойчивые подсистемные части; не наращивать монолиты.
2. Ввести transport-owned exact payload storage и сразу перевести far task, far channel и far select:
    - task result ссылается на canonical typed slot;
    - channel/transport владеет точными payload bytes;
    - select пишет в typed destination;
    - word-only ABI и adapters удаляются в том же изменении.

3. Закрыть caller-anchor/body-lease:
    - caller сохраняет owning anchor;
    - body получает generation-qualified non-owning pinned lease;
    - cancel/stale/shutdown освобождают pin и nested payload ровно один раз.

4. Реализовать DEBT-031 после DEBT-277:
    - reservation token: NONE → PROVISIONAL → COMMITTED → CONSUMED_REPLY | RELEASED_NO_REPLY;
    - reply resource резервируется до публикации request;
    - target refusal откатывает provisional reserve до park;
    - source exhaustion паркует source task, не удерживая target resource;
    - committed reply не может получить FULL;
    - обычный producer паркуется вместо пользовательского QUEUE_FULL.

5. Добавить точную resident-byte telemetry: header, padding, payload, sidecars и crossing clone.
    - Slot budgets остаются 64/16.
    - Normal/jumbo byte budgets сначала измеряются.
    - Если понадобятся новые численные лимиты, работа останавливается с таблицей измерений и отдельным owner-решением;
      implementer их не выбирает.

6. Закрыть P4 matrix:
    - > word values через far task/channel/select;
    - success, refusal, stale generation, cancel, shutdown и partial allocation;
    - DATA exhaustion, два jumbo producer, oversized reply;
    - under-budget Rule-13 mutant;
    - exact возврат request/reply slot и byte credits;
    - Close-wins для detached far claims.

7. Пройти sanitizer/Valgrind/TSan, behaviour/topology, resource counters, review и зафиксировать Wave E SHA.

## Этап 3 — Wave F и Epic 21 Task 9

1. Rule-14 audit сначала проверяет уже заявленные diagnostics/P5 свойства; исправлять только реальные gaps.
2. DEBT-309 закрывается доказательством по каждому из 31 pointer-returning entrypoint: valid result либо terminal
   RT_OOM/fatal, без nullable continuation.

3. DEBT-318:
    - удалить все 27 путей Frame.Locals → LocalSlot.V → Value;
    - удалить legacy carrier symbols, glue, flags и их тесты;
    - переснять live frozen census — не использовать исторические 0 of 626 или текущие 683;
    - единственная разрешённая word-like запись остаётся документированным fixnum representation.

4. Закрыть DEBT-125 и Epic 21:
    - property map migration/share/select/non-copy-channel × happy/cancel/refusal/teardown-buffered;
    - shards 1/2/8;
    - one-to-one mapping трёх исторических E2E tests или более сильные typed replacements;
    - Phase 5 seam inventory и dispositions DEBT-054–058 без реализации allocator Phase 5.

5. Исправить performance evidence 23b:
    - base и candidate SHA обязаны различаться;
    - оба выполняют общий manifest и одинаковый workload/checksum;
    - self-comparison и неподдерживаемый base fail closed.

6. Закрыть owned debts 031/056/062/080/082/125/126/133 только после focused evidence.
7. Финальный 23b closeout:
    - полный gate set дважды, goldens дважды;
    - sanitizer/Valgrind/TSan и paired benchmark;
    - Sentrux baseline/final;
    - пять независимых review-линз: model/liveness, ownership/types, runtime ABI, tests/gates, performance/docs;
    - синхронизировать README, PLAN, epic headers, DEBT и NOTES, не переписывая датированную историю.

8. После этого Epic 23b и Epic 21 получают COMPLETE.

## Этап 4 — Epic 22 Phase 2

1. Исправить устаревшую строку DEBT-035: authority — non-atomic RC + отсутствие shared heap block между shards.
2. Подтвердить, что шесть float/composite crossing barriers из Wave E закрыты, затем добавить int/uint.
3. Ownership:
    - fixnum не выделяет память и не вызывает retain/release;
    - heap local copy — O(1) retain;
    - move передаёт obligation;
    - drop делает release и освобождает на нуле;
    - copied crossing делает deep clone;
    - exclusive own crossing может передать единственный объект;
    - Range, composite, array, map, task, channel и select получают те же recursive lifecycle rules.

4. Проверить VM/LLVM parity, boundary integers, overflow, return/argument/local/container paths, cancellation и
   crossing cleanup.

5. Performance:
    - baseline — точный закрытый 23b SHA; candidate — отдельный Phase 2 SHA; равные SHA запрещены;
    - int/uint fixnum и fixed-width controls: 2 warmup + 7 alternating paired runs, CV каждой стороны ≤5%, throughput
      ≥95%, p95 ≤110%, zero allocations после setup;

    - heap-bignum: zero allocation на local copy, один retain на дополнительного owner, один eventual release,
      отсутствие local deep clone, crossing clone пропорционален limb payload, strict-zero и точный checksum;

    - heap timing публикуется, но не сравнивается с утечным baseline как acceptance gate.

6. Пройти полные compiler/runtime gates, sanitizer/Valgrind, benchmarks и независимый review; закрыть DEBT-035/068 и
   Epic 22.

## Операционная дисциплина и стоп-условия

- Перед каждым push проверять CI workflow triggers. Protocol commit сначала получает полный pre-push suite; после push
  дожидаемся автоматического CI и не дублируем покрываемые им проверки.

- Ryzen использовать только через ssh-ryzen.
- Функциональные независимые проверки можно запускать параллельно на 0–7 и 8–15; benchmark, Valgrind-sensitive и
  финальный stress — только эксклюзивно на 8–15.

- После старта серверного задания проверять unit, точную команду/SHA, PID/process tree, рост лога и CPU; один serial
  subtest может занимать одно ядро, но отсутствие прогресса обязано диагностироваться.

- После каждого integration/closeout сохранять в agentmemory: решение, инвариант, SHA, команду, результат, failure
  signature и ссылку на durable repo evidence.

- Board не обновлять. STATS.md, DEBT, NOTES и epic status обновлять по правилам проекта.
- Неблокирующий баг откладывать. Если баг блокирует gate — чинить до продолжения.
- Если модель не определяет неочевидное решение, остановиться и обсудить.
- Любой новый UX, синтаксис, language semantic или изменение согласованных численных transport budgets требует
  отдельного owner-решения.

- Финальный шаг — live-census всех epic headers и mandatory debts. Version bump не делать без отдельного решения.
