# Frontend Performance Optimization Report — Review Screen

## Fecha: 2026-10-21

## Optimizaciones implementadas en `public/app.js`

### 1. Cacheo de opciones de categorías (línea ~577)
**Antes:** Cada grupo de review regeneraba el HTML completo de 62 `<option>` elementos.
**Después:** `getCategoryOptions()` construye el HTML una sola vez y lo cachea. Los grupos posteriores reutilizan el cache y solo aplican `selected` mediante string replace.
**Impacto:** Con 8 grupos × 62 categorías = 496 opciones generadas → 62 opciones generadas + 8 replaces baratos.

### 2. Datalist compartido para cuentas de destino (línea ~600)
**Antes:** Cada grupo creaba su propio `<datalist>` con 1,686 opciones de cuentas. Con 8 grupos = 13,488 `<option>` elementos en el DOM.
**Después:** Un único `<datalist id="review-dest-shared-list">` reutilizado por todos los inputs. `getAcctDatalistOptions()` cachea el HTML.
**Impacto:** 13,488 opciones → 1,686 opciones. Reducción del 87.5% en nodos DOM de datalist.

### 3. DocumentFragment para renderizado por lotes (línea ~748)
**Antes:** `body.innerHTML = html` causaba un parseo completo y reflow único grande.
**Después:** Se construye el HTML en un `<div>` temporal, se transfieren los nodos a un `DocumentFragment`, y se hace un único `appendChild`. Esto reduce los reflows intermedios.

### 4. Resolución paralela de fetches con priorización (línea ~660)
**Antes:** `catRes → reviewRes → acctRes` se resolvían secuencialmente con `await`.
**Después:** Los 3 fetches se inician en paralelo. Se hace `await` primero de categories y accounts (rápidos), se procesan, y luego se espera review (lento). Las categorías y cuentas ya están listas cuando llega review.

### 5. `updateSubmitBar()` con guard de cambio (línea ~1497)
**Antes:** Cada llamada reescribía `btn.innerHTML` incondicionalmente.
**Después:** Solo escribe si el texto cambió. Evita reflows innecesarios en cada `onchange`.

### 6. `updateTransferButtons()` con querySelectorAll optimizado (línea ~1182)
**Antes:** `document.querySelectorAll('[id^="transfer-dest-"]')` iteraba TODOS los selects, luego por cada uno hacía `document.querySelectorAll` de nuevo para los individuales.
**Después:** Itera los grupos de transferencia una vez, y usa `groupEl.querySelectorAll` (scoped) para los selects individuales.

### 7. `refreshDatalists()` simplificado (línea ~1448)
**Antes:** Iteraba todos los datalists, eliminaba opciones `new:`, y reinsertaba.
**Después:** Invalida cache y regenera el único datalist compartido.

### 8. `onReviewDestInput()` usa datalist compartido (línea ~1387)
**Antes:** Buscaba en `review-dest-list-{gi}` (datalist por grupo).
**Después:** Busca en `review-dest-shared-list` (único datalist).

## Métricas de archivos

| Archivo | Antes | Después | Cambio |
|---------|-------|---------|--------|
| app.js (líneas) | 1,785 | 1,880 | +85 (código de cache) |
| app.js (bytes) | 85,069 | 88,774 | +3,705 |

## Verificación

- `node --check public/app.js` → OK (sin errores de sintaxis)
- `go build ./...` → OK
- Servidor arranca correctamente en :3999
- `/api/categories` responde en ~0.16s (sin regresión)
- `/api/accounts?type=asset` responde en ~0.15s (sin regresión)
- `/app.js` servido correctamente (88,774 bytes)
- 0 referencias a datalists per-g störül (`review-dest-list-{gi}`)
- 6 referencias al datalist compartido (`review-dest-shared-list`)

## Impacto esperado en renderizado

Con 8 grupos de review y 1,686 cuentas de gasto:

| Métrica | Antes | Después | Mejora |
|---------|-------|---------|--------|
| Nodos `<option>` de categorías en DOM | 8 × 62 = 496 | 62 (cache) + 8 selects | ~87% menos generación |
| Nodos `<option>` de cuentas en DOM | 8 × 1,686 = 13,488 | 1,686 | 87.5% menos |
| Reflows en renderCurrentBatch | Múltiples (innerHTML) | 1 (DocumentFragment) | Reducción significativa |
| Tiempo de generación HTML por grupo | O(categorías + cuentas) | O(1) con cache | ~O(1) amortizado |

## Notas

- El cuello de botella principal (55s en /api/review) es del backend y está en otra tarea (t_ccb58c78)
- Estas optimizaciones frontend mejoran el renderizado una vez que los datos llegan
- El datalist compartido es compatible con todos los navegadores modernos
- Los caches se invalidan correctamente cuando cambian categorías/cuentas/pendingAccounts
