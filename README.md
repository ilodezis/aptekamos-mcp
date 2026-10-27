<div align="center">

# aptekamos-mcp

[English](#english) · [Русский](#русский)

**An MCP server for [AptekaMos](https://aptekamos.ru), a Russian drug price and availability service.**<br>
**MCP-сервер для [АптекаМос](https://aptekamos.ru) — сервиса цен и наличия лекарств в аптеках России.**

![Go](https://img.shields.io/badge/go-1.27-00ADD8?logo=go&logoColor=white)
![MCP SDK](https://img.shields.io/badge/official%20MCP%20Go%20SDK-6E56CF)
![transport](https://img.shields.io/badge/transport-http%20%7C%20stdio-555)
![read--only](https://img.shields.io/badge/mode-read--only-2EA043)
[![tests](https://github.com/ilodezis/aptekamos-mcp/actions/workflows/tests.yml/badge.svg)](https://github.com/ilodezis/aptekamos-mcp/actions/workflows/tests.yml)
[![license](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

</div>

---

## English

Ask Claude where a drug is cheapest near you, what a pharmacy has on its shelf, what replaces a product that ran out, or which pharmacy has your whole list.

### What it is

AptekaMos collects the price lists of thousands of pharmacies: about 5,000 in Moscow and its oblast, plus 64 other regions. It has no public API. This server reads the site's own pages and turns them into 13 read-only tools. It is meant as a **remote connector**: Streamable HTTP behind a small single-user OAuth gate, so Claude.ai can connect by URL and agents can use a Bearer token. stdio works too, for local use.

### Tools

| Tool | What it answers |
|---|---|
| `search_drugs` | Find products by trade name or active substance; ids, active substance, group, how many pharmacies sell each, price range |
| `drug_card` | What a product is: other dosages, pack sizes, countries, manufacturers (ids for filters), price range, pharmacy count; for an exact `drug_id` every pack size has its own pharmacy count, prices, units and price per unit |
| `drug_instruction` | The leaflet: composition, indications, dosage, contraindications, side effects, storage, ATC, ICD-10, sources |
| `where_to_buy` | Pharmacies selling a product: price, address, metro, phones, in stock / reserve / delivery / 24h / sale / online order; filters by location, metro, chain, street, pack, maker, price; sorting |
| `nearest_pharmacies` | Closest pharmacies to a point with distance and the product's lowest price there; covers every matching pharmacy, not just the first 250 table rows |
| `price_at_pharmacy` | The exact packs of a product in one pharmacy and their prices; the same pack held at several prices is one item (`price` = lowest, `prices` = all), `units` and `unit_price` for packs of 2+ |
| `pharmacy_card` | Address, directions, phones, hours, open now, price list date and size, order terms, coordinates |
| `pharmacy_price_list` | Search one pharmacy's whole price list |
| `find_pharmacies` | Pharmacies of a region by location, metro, chain, street, name, features |
| `substitutes` | Other brands and forms with the same active substance |
| `browse` | Everything for an active substance, a pharmacological group (its own node of the site tree, not all analogues) or a brand |
| `shopping_list` | Pharmacies that have every product of a list, with the total price there |
| `reference` | Ids for filters: regions, cities and districts, metro, chains, streets |

Two kinds of product id: `drug_id` is one exact product (brand, form and dosage); `brand_id` + `form_id` is a brand in one dosage form across its dosages. A pharmacy is `pharmacy_id` in every answer. Product rows nobody sells have no basket link on the site, so their `brand_id`/`form_id` are filled in from rows of the same brand and form, or from the product's card. Prices are in roubles, straight from the pharmacies' price lists.

#### Response shape

Answers are kept small, since a model pays for every character:

- `search_drugs`, `browse` and `substitutes` say what every returned row shares (`kind`, `inn`, `inn_id`, `group`, `group_id`) once, in `common`, and leave it out of the rows; a field that differs between rows stays in the rows. Rows carry no link: a product page is `https://aptekamos.ru/tovary/lekarstva/x-1/x-<drug_id>/ceni` (the site redirects to the canonical address; checked for drugs, supplements and medical devices). The top-level `url` stays.
- A product nobody sells now is a short row, `{drug_id, brand_id, form_id, name, status}`; the `status` ("нет в наличии"…) is what marks it. `available_only=true` drops such rows before paging, so `products_total` counts what is given out.
- `where_to_buy` returns `product` as a short header (`drug_id`, `brand_id`, `form_id`, `name`, `pharmacies`, `price_min`, `price_max`); `include_product=true` gives the whole card (dosages, packs, countries, manufacturers).
- `drug_instruction` returns an overview by default: every section's title and its first 300 characters (`full_chars` is the real length of a cut one), the ATC code, and ICD-10 codes, conditions and sources when they are short. `sections=["дозиров", "противопоказ"]` returns the full text of the sections whose title contains a word (case-insensitive) together with ICD-10, conditions and sources; `max_chars` cuts each section. On the tirzepatide leaflet this is about 4.5k characters of JSON instead of 23k.
- Lists are always arrays, never `null`. An empty answer carries a `hint`: a name typed in Latin letters only is retried once in Cyrillic (`sedzharo` finds «Седжаро»), and if nothing is found `search_drugs` says to write the name in Russian; `reference(kind=networks)` with no match points to `find_pharmacies(pharmacy_query=…)`, because a lone pharmacy is not a chain.
- `pharmacy_price_list` searches by trade name, not by active substance (`тирзепатид` finds nothing, `седжаро` does).

### Example

> *"Where is Mexidol 125 mg cheapest near Belorusskaya, and is the pharmacy open now?"*

Claude calls `search_drugs("мексидол")`, takes the tablet's `drug_id`, calls `nearest_pharmacies` at the station's coordinates, then `pharmacy_card` for the winner, and answers with the price, distance, address and hours.

### Running

```bash
go build -o aptekamos-mcp .
./aptekamos-mcp                 # stdio
ADDR=:8130 PASSWORD=… TOKEN_SECRET=… CLIENT_SECRET=… ./aptekamos-mcp   # HTTP at /mcp
```

Or with Docker: copy `.env.example` to `.env`, fill it in, and run `docker compose up -d --build`. The compose file binds to `127.0.0.1:8130`; put a TLS reverse proxy in front.

### Remote mode (Claude.ai)

In HTTP mode `/mcp` needs a Bearer token. The server is its own OAuth provider for a single owner:

1. Set `PASSWORD`, `TOKEN_SECRET` (`openssl rand -base64 48`) and `CLIENT_SECRET` in `.env`; `CLIENT_ID` defaults to `aptekamos-claude-connector`.
2. In Claude.ai: Settings → Connectors → Add custom connector, URL `https://<your host>/mcp`, and in advanced settings the client id and secret.
3. Claude opens the consent page; type the password once. Tokens live a year.

For agents that cannot do OAuth, mint a token by hand on the server: `aptekamos-mcp sign-token --days 365`, then for example

```bash
claude mcp add --transport http aptekamos https://<your host>/mcp --header "Authorization: Bearer <token>"
```

Rotating `TOKEN_SECRET` revokes every token. `/healthz` answers without a token.

### How the site works

- Pages are server-rendered HTML; the "AJAX" endpoints return ready HTML fragments (`?pm=modal`), and the few JSON ones only tell the front end which fragment to load next.
- Links hide in `data-go-to-link`, `data-link` and `<option value>` as base64 of the absolute URL.
- Only the trailing ids of a path matter: `/tovary/lekarstva/x-1/x-13270/ceni` redirects to the canonical slugs. The server builds every URL from ids.
- Regions are path prefixes (`/spb/`, `/ekb/`, `/samara/`…); cities and districts are a `r=` filter. Metro, chains and streets are `me=`, `mn=`, `st=` id lists.
- Pharmacy tables fold chains into one row and stop at 250 rows. The map modal of the same page carries a JSON array of **every** matching pharmacy with coordinates, opening status and lowest price; `nearest_pharmacies` sorts that by distance.
- The basket is stateless: `/korzina?aid=bsw&productids=<keys>` lists pharmacies for the whole list, where the keys are encrypted product ids from the product pages.

### Limitations

- Unofficial: when AptekaMos changes its markup, tools break until the parsers are updated. The weekly live tests are there to notice.
- Tables show at most 250 rows; use filters, or `nearest_pharmacies` for a location.
- The site's substance page and substitutes list leave out products withdrawn from circulation (status "Отсутствует в обороте"), while search shows them: `browse(kind=inn)` and `substitutes` can list a few products fewer than `search_drugs` for the same substance. `substitutes` also leaves out the product it was asked about.
- Prices and stock are as fresh as each pharmacy's price list (`price_list_date`), usually hours.
- A price range spans every pack size and pharmacy. In lists (`search_drugs`, `browse`, `substitutes`) one top-level `price_note` says so; `drug_card` gives each pack of an exact drug its own range and price per unit; `where_to_buy` adds a `price_note` listing the packs when no `pack` filter is set. A pharmacy sometimes holds one pack at several prices (different batches, probably; the site does not say), and `price_at_pharmacy` and `pharmacy_price_list` show it once, with the lowest price and all of them in `prices`.
- A pharmacological group is its own node of the site tree and holds only the products filed under it: group 1159 has tirzepatide and two gliflozins, while metformin sits in groups 246, 906 and 994. Take `group_id` from a product, do not guess it; `browse` by `inn` lists one substance.
- The client keeps no cookies or session: every page is fetched the same way for everyone.
- No ordering or reservations: those need the buyer's phone number and are writes.
- The server asks the site at most twice a second and caches pages (10 minutes for prices, a day for leaflets and dictionaries).

### Tests

```bash
OFFLINE=1 go test ./...   # parsers and tools against saved pages (internal/aptekamos/testdata)
go test ./...             # plus live tests against aptekamos.ru, about 35 s
```

### Disclaimer

Not affiliated with AptekaMos or its owner. For personal, non-commercial use; respect the site's terms and keep the request rate low. The leaflet text is reference information from the site, **not medical advice**: prescription drugs and doses are a doctor's call.

---

## Русский

Спросите Claude, где лекарство дешевле рядом с вами, что есть в конкретной аптеке, чем заменить закончившийся препарат или в какой аптеке есть весь ваш список.

### Что это

АптекаМос собирает прайс-листы тысяч аптек: около 5000 в Москве и области и ещё 64 региона. Публичного API у сайта нет. Сервер читает страницы самого сайта и превращает их в 13 инструментов только для чтения. Задуман как **удалённый коннектор**: Streamable HTTP за маленьким однопользовательским OAuth, чтобы Claude.ai подключался по ссылке, а агенты ходили с Bearer-токеном. Локально работает и через stdio.

### Инструменты

| Инструмент | На что отвечает |
|---|---|
| `search_drugs` | Поиск по торговому названию или действующему веществу: id, МНН, группа, во скольких аптеках есть, вилка цен |
| `drug_card` | Что за препарат: другие дозировки, фасовки, страны, производители (id для фильтров), цены, число аптек; для точного `drug_id` у каждой фасовки свои число аптек, цены, штук в упаковке и цена за штуку |
| `drug_instruction` | Инструкция: состав, показания, дозировка, противопоказания, побочные действия, хранение, АТХ, МКБ-10, источники |
| `where_to_buy` | Аптеки с препаратом: цена, адрес, метро, телефоны, наличие / бронь / доставка / круглосуточно / акция / онлайн-заказ; фильтры по месту, метро, сети, улице, фасовке, производителю, цене; сортировка |
| `nearest_pharmacies` | Ближайшие к точке аптеки с расстоянием и минимальной ценой препарата; по всем подходящим аптекам, а не по первым 250 строкам таблицы |
| `price_at_pharmacy` | Конкретные упаковки препарата в одной аптеке и их цены; одна фасовка по нескольким ценам сводится в одну позицию (`price` — минимальная, `prices` — все), `units` и `unit_price` для упаковок от 2 шт. |
| `pharmacy_card` | Адрес, как пройти, телефоны, часы работы, открыта ли сейчас, дата и размер прайса, условия заказа, координаты |
| `pharmacy_price_list` | Поиск по всему прайсу одной аптеки |
| `find_pharmacies` | Аптеки региона по месту, метро, сети, улице, названию, услугам |
| `substitutes` | Другие торговые названия и формы с тем же действующим веществом |
| `browse` | Всё по МНН, фармгруппе (это отдельный узел дерева сайта, а не все аналоги) или бренду |
| `shopping_list` | Аптеки, где есть весь список, и сколько он там стоит |
| `reference` | Id для фильтров: регионы, города и округа, метро, сети, улицы |

Два вида id препарата: `drug_id` — конкретный товар (бренд, форма, дозировка); `brand_id` + `form_id` — бренд в одной лекарственной форме во всех дозировках. Аптека везде называется `pharmacy_id`. У строк, которые никто не продаёт, на сайте нет кнопки корзины, поэтому их `brand_id`/`form_id` подставляются по строкам того же бренда и формы или по карточке товара. Цены в рублях, прямо из прайсов аптек.

#### Форма ответов

Ответы короткие: модель платит за каждый символ.

- `search_drugs`, `browse` и `substitutes` один раз отдают в `common` то, что одинаково у всех выданных строк (`kind`, `inn`, `inn_id`, `group`, `group_id`), и убирают это из строк; поле, которое отличается между строками, остаётся в строках. Ссылок в строках нет: страница товара — `https://aptekamos.ru/tovary/lekarstva/x-1/x-<drug_id>/ceni` (сайт редиректит на канонический адрес; проверено на лекарствах, БАДах и изделиях). `url` верхнего уровня остаётся.
- Товар, которого сейчас ни у кого нет, — короткая строка `{drug_id, brand_id, form_id, name, status}`; недоступность видна по `status` («нет в наличии»…). `available_only=true` убирает такие строки до пагинации, поэтому `products_total` считает то, что выдано.
- `where_to_buy` отдаёт `product` коротким заголовком (`drug_id`, `brand_id`, `form_id`, `name`, `pharmacies`, `price_min`, `price_max`); `include_product=true` возвращает всю карточку (дозировки, фасовки, страны, производители).
- `drug_instruction` по умолчанию даёт обзор: заголовок и первые 300 символов каждого раздела (`full_chars` — настоящая длина обрезанного), код АТХ, а МКБ-10, состояния и источники — если они короткие. `sections=["дозиров", "противопоказ"]` отдаёт полный текст разделов, в заголовке которых есть слово (без учёта регистра), вместе с МКБ-10, состояниями и источниками; `max_chars` обрезает каждый раздел. На листке тирзепатида это около 4,5 тыс. символов JSON вместо 23 тыс.
- Списки всегда массивы, не `null`. У пустого ответа есть `hint`: название только латиницей повторяется один раз кириллицей (`sedzharo` находит «Седжаро»), а если и так пусто, `search_drugs` советует писать по-русски; `reference(kind=networks)` без совпадений отправляет к `find_pharmacies(pharmacy_query=…)`: одиночная аптека — не сеть.
- `pharmacy_price_list` ищет по торговому названию, а не по МНН (`тирзепатид` даёт 0, `седжаро` находит).

### Пример

> *«Где дешевле всего мексидол 125 мг возле Белорусской, и открыта ли аптека сейчас?»*

Claude вызывает `search_drugs("мексидол")`, берёт `drug_id` таблеток, вызывает `nearest_pharmacies` с координатами станции, затем `pharmacy_card` для лучшей аптеки и отвечает ценой, расстоянием, адресом и часами работы.

### Запуск

```bash
go build -o aptekamos-mcp .
./aptekamos-mcp                 # stdio
ADDR=:8130 PASSWORD=… TOKEN_SECRET=… CLIENT_SECRET=… ./aptekamos-mcp   # HTTP на /mcp
```

Или в Docker: скопируйте `.env.example` в `.env`, заполните и запустите `docker compose up -d --build`. Compose слушает `127.0.0.1:8130`, TLS делает обратный прокси.

### Удалённый режим (Claude.ai)

В HTTP-режиме `/mcp` требует Bearer-токен. Сервер сам себе OAuth-провайдер для одного владельца:

1. Задайте в `.env` `PASSWORD`, `TOKEN_SECRET` (`openssl rand -base64 48`) и `CLIENT_SECRET`; `CLIENT_ID` по умолчанию `aptekamos-claude-connector`.
2. В Claude.ai: Settings → Connectors → Add custom connector, URL `https://<ваш хост>/mcp`, в расширенных настройках client id и secret.
3. Claude откроет страницу согласия; пароль вводится один раз. Токен живёт год.

Агентам без OAuth выпустите токен на сервере: `aptekamos-mcp sign-token --days 365`, например

```bash
claude mcp add --transport http aptekamos https://<ваш хост>/mcp --header "Authorization: Bearer <токен>"
```

Смена `TOKEN_SECRET` отзывает все токены. `/healthz` отвечает без токена.

### Как устроен сайт

- Страницы рендерятся на сервере; «AJAX»-ручки отдают готовые HTML-куски (`?pm=modal`), а редкие JSON-ответы только говорят фронту, какой кусок грузить дальше.
- Ссылки спрятаны в `data-go-to-link`, `data-link` и `<option value>` как base64 от абсолютного URL.
- В пути важны только id в конце сегментов: `/tovary/lekarstva/x-1/x-13270/ceni` редиректит на канонический адрес. Сервер собирает все URL из id.
- Регион — префикс пути (`/spb/`, `/ekb/`, `/samara/`…), города и округа — фильтр `r=`. Метро, сети и улицы — списки id `me=`, `mn=`, `st=`.
- Таблицы аптек сворачивают сети в одну строку и обрываются на 250 строках. Модалка карты той же страницы несёт JSON-массив **всех** подходящих аптек с координатами, статусом работы и минимальной ценой; `nearest_pharmacies` сортирует его по расстоянию.
- Корзина без состояния: `/korzina?aid=bsw&productids=<ключи>` показывает аптеки под весь список; ключи — зашифрованные id со страниц препаратов.

### Ограничения

- Неофициально: когда АптекаМос меняет вёрстку, инструменты ломаются до обновления парсеров. Еженедельные живые тесты нужны, чтобы это заметить.
- Таблицы показывают не больше 250 строк; сужайте фильтрами или берите `nearest_pharmacies` для конкретного места.
- Страница МНН и список замен на сайте не показывают выведенные из оборота товары (статус «Отсутствует в обороте»), а поиск показывает: `browse(kind=inn)` и `substitutes` могут дать на несколько товаров меньше, чем `search_drugs` по тому же веществу. `substitutes` ещё и не включает сам товар, для которого искали замены.
- Цены и наличие свежи настолько, насколько свеж прайс аптеки (`price_list_date`), обычно это часы.
- Диапазон цены охватывает все фасовки и все аптеки. В списках (`search_drugs`, `browse`, `substitutes`) это говорит один `price_note` наверху ответа; `drug_card` даёт каждой фасовке точного препарата свой диапазон и цену за штуку; `where_to_buy` без фильтра `pack` добавляет `price_note` с перечнем фасовок. Аптека иногда держит одну фасовку по нескольким ценам (вероятно, разные партии; сайт не объясняет), `price_at_pharmacy` и `pharmacy_price_list` показывают её одной позицией: `price` — минимальная, `prices` — все.
- Фармгруппа — отдельный узел дерева сайта и содержит только приписанные к ней товары: в группе 1159 тирзепатид и два глифлозина, а метформин лежит в группах 246, 906 и 994. `group_id` берите из товара, не угадывайте; все товары одного вещества даёт `browse` по `inn`.
- Клиент не хранит cookie и сессию: каждая страница запрашивается одинаково для всех.
- Заказа и брони нет: для них нужен телефон покупателя, и это запись.
- Сервер ходит на сайт не чаще двух раз в секунду и кэширует страницы (цены на 10 минут, инструкции и справочники на сутки).

### Тесты

```bash
OFFLINE=1 go test ./...   # парсеры и инструменты на сохранённых страницах (internal/aptekamos/testdata)
go test ./...             # плюс живые тесты против aptekamos.ru, около 35 с
```

### Дисклеймер

Проект не связан с АптекаМос и его владельцем. Для личного некоммерческого использования; уважайте правила сайта и не повышайте частоту запросов. Текст инструкций — справочная информация с сайта, **не медицинская рекомендация**: рецептурные препараты и дозы назначает врач.

---

## License / Лицензия

[MIT](LICENSE)
