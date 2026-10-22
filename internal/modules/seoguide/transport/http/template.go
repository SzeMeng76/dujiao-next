package seoguidehttp

// guideTmplSrc 是 /guide 系列页面的 HTML 模板。
// 故意保持简洁：无任何框架依赖，一次 template.Parse 就能用。
// 设计原则：
//   - 每个商品条目都是带 href 的真实 <a> 标签，让爬虫能发现 /products/:slug
//   - 分类导航同样是真实内链，帮助爬虫发现 /guide/c/:slug 和 /categories/:slug
//   - 有真实可见的正文（标题、描述、价格），让 Google 判定为有内容的页面
//   - 不做任何 JavaScript，不依赖前端渲染
const guideTmplSrc = `<!doctype html>
<html lang="{{if .Lang}}{{.Lang}}{{else}}zh-CN{{end}}">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>{{.Title}}</title>
  {{- if .Description}}
  <meta name="description" content="{{.Description}}">
  {{- end}}
  {{- if .Keywords}}
  <meta name="keywords" content="{{.Keywords}}">
  {{- end}}
  {{- if .CanonicalURL}}
  <link rel="canonical" href="{{safeURL .CanonicalURL}}">
  {{- end}}
  {{- if .Title}}
  <meta property="og:title" content="{{.Title}}">
  {{- end}}
  {{- if .Description}}
  <meta property="og:description" content="{{.Description}}">
  {{- end}}
  {{- if .CanonicalURL}}
  <meta property="og:url" content="{{safeURL .CanonicalURL}}">
  <meta property="og:type" content="website">
  {{- end}}
  {{- if .SiteName}}
  <meta property="og:site_name" content="{{.SiteName}}">
  {{- end}}
  <style>
    body{font-family:system-ui,sans-serif;max-width:1100px;margin:0 auto;padding:16px 20px;color:#222}
    h1{font-size:1.5rem;margin-bottom:8px}
    nav.cats{display:flex;flex-wrap:wrap;gap:8px;margin-bottom:24px}
    nav.cats a{padding:4px 12px;border:1px solid #ccc;border-radius:20px;text-decoration:none;color:#0066cc;font-size:.9rem}
    nav.cats a.active{background:#0066cc;color:#fff;border-color:#0066cc}
    .products{display:grid;grid-template-columns:repeat(auto-fill,minmax(220px,1fr));gap:16px}
    .product-card{border:1px solid #eee;border-radius:8px;padding:14px;display:flex;flex-direction:column}
    .product-card a{text-decoration:none;color:inherit}
    .product-card img{width:100%;height:140px;object-fit:cover;border-radius:4px;margin-bottom:8px}
    .product-card .title{font-weight:600;font-size:.95rem;margin-bottom:4px}
    .product-card .desc{font-size:.82rem;color:#555;flex:1;margin-bottom:8px;
      display:-webkit-box;-webkit-line-clamp:3;-webkit-box-orient:vertical;overflow:hidden}
    .product-card .price{font-weight:700;color:#c0392b;font-size:1rem}
    .posts{list-style:none;padding:0;margin:0}
    .posts li{padding:6px 0;border-bottom:1px solid #f0f0f0}
    .posts li a{color:#0066cc;text-decoration:none}
    h2{font-size:1.1rem;margin:28px 0 10px}
    .back{margin-bottom:16px;display:inline-block;color:#0066cc}
    footer{margin-top:40px;font-size:.8rem;color:#999;border-top:1px solid #eee;padding-top:12px}
  </style>
</head>
<body>
  {{if .SiteName}}<h1><a href="{{safeURL .SiteURL}}">{{.SiteName}}</a></h1>{{end}}

  {{if .CurrentCategory}}
  <a class="back" href="{{safeURL .SiteURL}}/guide">{{.Labels.BackToAll}}</a>
  <h2>{{.Labels.CategoryPre}}{{.CurrentCategory.Name}}</h2>
  {{end}}

  {{if .Categories}}
  <nav class="cats">
    {{range .Categories}}
    <a href="{{safeURL .GuideURL}}"{{if $.CurrentCategory}}{{if eq $.CurrentCategory.Slug .Slug}} class="active"{{end}}{{end}}>{{.Name}}</a>
    {{end}}
  </nav>
  {{end}}

  {{if .Products}}
  <div class="products">
    {{range .Products}}
    <div class="product-card">
      <a href="{{safeURL .DetailURL}}">
        {{if .Image}}<img src="{{safeURL .Image}}" alt="{{.Title}}" loading="lazy">{{end}}
        <div class="title">{{.Title}}</div>
        {{if .Description}}<div class="desc">{{.Description}}</div>{{end}}
        <div class="price">{{.Currency}} {{.PriceAmount}}</div>
      </a>
    </div>
    {{end}}
  </div>
  {{end}}

  {{if .Posts}}
  <h2>{{.Labels.LatestPosts}}</h2>
  <ul class="posts">
    {{range .Posts}}
    <li><a href="{{safeURL .PostURL}}">{{.Title}}</a></li>
    {{end}}
  </ul>
  {{end}}

  <footer>
    <a href="{{safeURL .SiteURL}}/guide">{{.Labels.GuideIndex}}</a>
    &nbsp;·&nbsp;
    <a href="{{safeURL .SiteURL}}/sitemap.xml">Sitemap</a>
  </footer>
</body>
</html>`
