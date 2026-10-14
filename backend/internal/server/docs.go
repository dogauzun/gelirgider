package server

import "net/http"

// docsPage is the Swagger UI page. The UI itself loads from the CDN, so no
// UI asset ships in the binary (ADR 0013). swagger-ui-dist is pinned to
// 5.33.0 with SRI: a version bump changes both URLs and both integrity
// hashes (sha384 of the exact file in the npm tarball).
const docsPage = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>gelirgider API</title>
<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5.33.0/swagger-ui.css" integrity="sha384-Ov4/wv3j2bmct8cDc5X4ngJZohVPzEmc6uDPH8WeljUxO5vtoykvMEfbu9Vh6RaW" crossorigin="anonymous">
</head>
<body>
<div id="swagger-ui"></div>
<script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5.33.0/swagger-ui-bundle.js" integrity="sha384-YDALVcy8kj8yltLBVi1vBiBAUqdxvus673gM8XKwiy6aDUJFXivF/KCufekjYbVf" crossorigin="anonymous"></script>
<script>
SwaggerUIBundle({ url: "/api/docs/swagger.json", dom_id: "#swagger-ui" });
</script>
</body>
</html>
`

// Docs serves Swagger UI at /api/docs/ and the files `make swagger`
// generates into dir (swagger.json, swagger.yaml) below it. Dev only: main
// mounts it when API_DOCS_DIR is set and COOKIE_SECURE is not (ADR 0013).
func Docs(dir string) http.Handler {
	files := http.StripPrefix("/api/docs/", http.FileServer(http.Dir(dir)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/docs/" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(docsPage))
			return
		}
		files.ServeHTTP(w, r)
	})
}
