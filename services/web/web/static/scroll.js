// Infinite スクロール。一覧末尾の「次へ」リンクが見えたら続きの HTML 断片を
// 取得して追記する。JS が無効でも「次へ」リンクで次のページへ進める。
(() => {
  const list = document.getElementById("todos");
  if (!list || !("IntersectionObserver" in window)) return;

  let loading = false;
  const observer = new IntersectionObserver(async (entries) => {
    if (loading || !entries.some((e) => e.isIntersecting)) return;
    const more = document.getElementById("more");
    if (!more) return;

    loading = true;
    try {
      const res = await fetch(more.dataset.page, { credentials: "same-origin" });
      // セッション切れはサーバーが /login へリダイレクトする
      if (res.redirected) {
        location.href = res.url;
        return;
      }
      if (!res.ok) return;
      const html = await res.text();
      observer.disconnect();
      document.getElementById("more-item").remove();
      list.insertAdjacentHTML("beforeend", html);
      const next = document.getElementById("more");
      if (next) observer.observe(next);
    } finally {
      loading = false;
    }
  });

  const more = document.getElementById("more");
  if (more) observer.observe(more);
})();
