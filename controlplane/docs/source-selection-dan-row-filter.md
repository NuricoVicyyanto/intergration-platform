# Panduan Source Selection dan Row Filter

Panduan ini menjelaskan cara membatasi data yang dipindahkan oleh pipeline dari
UI **Pipelines** pada bagian **Source selection**.

Source selection berguna ketika pipeline tidak boleh mengambil semua tabel atau
semua baris. Tersedia dua tingkat filter:

1. **Tables or streams** menentukan stream/tabel yang ikut diproses.
2. **Row filter** menentukan record dalam stream terpilih yang boleh masuk ke
   target.

Kedua field bersifat opsional. Jika keduanya kosong, pipeline memindahkan semua
stream dan semua record yang tersedia.

## Melihat field di UI

Buka:

1. **Pipelines**
2. **New pipeline** atau tombol edit pada pipeline yang sudah ada.
3. Pilih source dan destination connection.
4. Isi bagian **Source selection**.
5. Simpan pipeline.

Konfigurasi juga tersedia melalui API:

```json
{
  "source_filter": {
    "streams": ["appdb-employees"],
    "row_filter": "is_active == True"
  }
}
```

## Mengisi Tables or streams

Field **Tables or streams** menerima satu atau beberapa pola. Di UI, pisahkan
dengan koma atau baris baru.

### Satu tabel

```text
appdb-employees
```

Untuk PostgreSQL, Meltano umumnya membentuk nama stream dengan format
`schema-table`:

```text
public-employees
```

Nama yang ditulis harus sama dengan nama stream yang dikenali connector. Nama
stream tidak selalu sama dengan nama tabel SQL. Contohnya, `tap-mysql` pada
workspace ini menghasilkan stream:

```text
appdb-employees
```

### Beberapa tabel

```text
appdb-employees
appdb-orders
appdb-invoices
```

Atau satu baris dipisahkan koma:

```text
appdb-employees, appdb-orders, appdb-invoices
```

### Wildcard

Pola wildcard mengikuti pola nama file:

- `*` cocok dengan nol atau lebih karakter.
- `?` cocok dengan satu karakter.
- `[abc]` cocok dengan salah satu karakter di dalam kurung.

Contoh:

```text
appdb-*
```

Contoh di atas memindahkan semua stream yang namanya diawali `appdb-`.

```text
public-orders-*
```

Contoh tersebut dapat digunakan untuk memilih beberapa stream yang namanya
diawali `public-orders-`.

### Mencari nama stream yang benar

Nama stream terbaik dilihat dari hasil discovery atau log eksekusi:

1. Jalankan pipeline tanpa source selection terlebih dahulu, atau gunakan
   pipeline uji.
2. Buka panel **Execution trace** di Overview.
3. Cari log seperti:

   ```text
   Beginning full_table sync of 'appdb-employees'...
   ```

4. Gunakan nama di dalam tanda kutip pada field **Tables or streams**.

Perintah CLI berikut juga dapat menampilkan stream yang tersedia:

```powershell
cd C:\Users\User\Documents\devel\meltano\controlplane\.cache\tenants\acme
C:\Users\User\Documents\devel\meltano\.venv\Scripts\meltano.exe select tap-mysql --all
```

Ganti `tap-mysql` dengan extractor yang digunakan. Discovery melalui CLI dapat
memerlukan environment kredensial source yang sama dengan worker.

## Mengisi Row filter

Field **Row filter** menerima **ekspresi stream-map Singer**, bukan query SQL.
Filter ini diterapkan pada setiap record yang keluar dari extractor dan sebelum
record masuk ke loader.

### Contoh umum

Boolean:

```text
is_active == True
```

String:

```text
status == "active"
```

Kombinasi beberapa kondisi:

```text
status == "active" and amount >= 100
```

Beberapa alternatif nilai:

```text
region in ["JKT", "SBY"]
```

Negasi:

```text
not is_deleted
```

### Aturan sintaks

- Gunakan `==` untuk perbandingan sama dengan. Jangan gunakan `=` seperti pada
  SQL.
- Gunakan `!=` untuk tidak sama dengan.
- Gunakan `>`, `>=`, `<`, dan `<=` untuk perbandingan angka.
- Gunakan `and`, `or`, dan `not` untuk logika boolean.
- String harus diapit tanda kutip, misalnya `"active"` atau `'active'`.
- Angka tidak perlu tanda kutip, misalnya `amount >= 100`.
- Boolean menggunakan `True` atau `False`, bukan `true` atau `false`.
- Nama field harus sama persis dengan field pada record source.
- Row filter maksimal 1.000 karakter dan tidak boleh berisi newline atau `;`.

## Contoh lengkap

### MySQL: satu tabel, hanya karyawan aktif

**Tables or streams**

```text
appdb-employees
```

**Row filter**

```text
is_active == True
```

Hasil pengujian pada workspace ini:

```text
Status          : succeeded
Stream          : appdb-employees
Records read    : 1.010
Records loaded  : 509
```

Extractor membaca record dari tabel, kemudian filter membuang record yang tidak
memenuhi `is_active == True` sebelum loader menulisnya.

### PostgreSQL: dua tabel dengan filter status

**Tables or streams**

```text
public-orders
public-customers
```

**Row filter**

```text
status == "active"
```

Filter yang sama diterapkan ke kedua stream. Pastikan field `status` tersedia di
kedua record. Jika tidak tersedia, evaluasi filter dapat gagal dan run akan
berstatus failed.

### SQL Server: memilih stream berdasarkan wildcard

**Tables or streams**

```text
sales-*
```

**Row filter**

```text
amount >= 1000 and region == "JKT"
```

### MongoDB: memilih collection dan dokumen aktif

**Tables or streams**

```text
users
orders
```

**Row filter**

```text
active == True
```

Untuk MongoDB, **Tables or streams** berisi nama collection atau stream yang
muncul pada log discovery `tap-mongodb`.

### Oracle: memilih tabel tanpa row filter

**Tables or streams**

```text
APP.EMPLOYEES
APP.DEPARTMENTS
```

Pemilihan tabel Oracle akan diterjemahkan menjadi nama stream
`APP-EMPLOYEES` dan `APP-DEPARTMENTS`.

Plugin `tap-oracle` legacy belum mendukung **Row filter**. API akan menolak row
filter untuk Oracle dengan HTTP `422`.

## Batasan penting

- Row filter tidak dikirim sebagai `WHERE` ke database source. Record tetap
  dibaca extractor lalu difilter sebelum masuk target.
- Query bebas seperti `SELECT ... JOIN ... GROUP BY ...` tidak didukung.
- Join, agregasi, dan pemilihan kolom dilakukan di tahap transformasi terpisah,
  bukan pada Source selection pipeline ini.
- Satu row filter berlaku untuk semua stream pada pipeline tersebut.
- Maksimal 100 pola stream, masing-masing maksimal 255 karakter.
- `tap-postgres`, `tap-mysql`, `tap-mssql`, dan `tap-mongodb` mendukung stream
  selection dan row filter.
- `tap-oracle` hanya mendukung table selection.

## Melihat hasil filter

Setelah run selesai, buka **Overview** lalu pilih run pada **Recent runs**.
Panel **Execution trace** menampilkan:

- stream yang diproses;
- jumlah record yang diekstrak;
- jumlah record yang dimuat;
- log eksekusi.

Perbedaan antara **Records extracted** dan **Records loaded** menunjukkan jumlah
record yang dibuang oleh row filter.

## Troubleshooting

### Semua tabel masih diproses

- Pastikan field **Tables or streams** tidak kosong.
- Periksa kembali nama stream. Gunakan nama persis dari log discovery.
- Wildcard harus sesuai format nama stream, bukan selalu nama tabel SQL.

### Pipeline gagal saat mengevaluasi filter

- Gunakan `==`, bukan `=`.
- Periksa huruf besar/kecil nama field.
- Untuk boolean gunakan `True` atau `False`.
- Pastikan field yang dipakai tersedia pada semua stream terpilih.
- Jangan gunakan query SQL seperti `WHERE`, `SELECT`, atau `JOIN`.

### Data terpilih tetapi tidak ada record yang dimuat

- Filter mungkin terlalu ketat.
- Tipe nilai mungkin berbeda. Misalnya `amount` tersimpan sebagai string,
  sehingga perbandingan dengan angka dapat memberikan hasil yang tidak
  diharapkan.
- Lihat bagian **Records extracted** dan **Records loaded** pada Execution trace.

### Oracle menolak Row filter

Ini adalah batasan plugin Oracle. Gunakan table selection atau lakukan filter di
tahap transformasi setelah data masuk ke destination.
