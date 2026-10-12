// Infinite スクロール。一覧末尾の「次へ」リンクが見えたら続きの HTML 断片を
// 取得して追記する。JS が無効でも「次へ」リンクで次のページへ進める。
(() => {
  const list = document.getElementById("todos");
  if (!list || !("IntersectionObserver" in window)) return;

  const MIN_DELAY = 1000;
  const MAX_DELAY = 30000;
  let loading = false;
  let retryDelay = MIN_DELAY;

  // 取得に失敗したら、間隔を空けて監視し直す。
  // IntersectionObserver は見え方が変わったときにしか通知しないため、リンクが
  // 表示されたままだと二度と通知されない。observe し直すと現在の状態が通知されるので、
  // まだ見えていればそこで再取得が走る。
  const retry = (more) => {
    observer.unobserve(more);
    setTimeout(() => {
      if (more.isConnected) observer.observe(more);
    }, retryDelay);
    retryDelay = Math.min(retryDelay * 2, MAX_DELAY);
  };

  const load = async (more) => {
    loading = true;
    try {
      const res = await fetch(more.dataset.page, { credentials: "same-origin" });
      // セッション切れはサーバーが /login へリダイレクトする
      if (res.redirected) {
        location.href = res.url;
        return;
      }
      if (!res.ok) throw new Error(`status ${res.status}`);
      const html = await res.text();

      observer.unobserve(more);
      document.getElementById("more-item").remove();
      list.insertAdjacentHTML("beforeend", html);
      retryDelay = MIN_DELAY;
      const next = document.getElementById("more");
      if (next) observer.observe(next);
    } catch (err) {
      console.warn("一覧の続きを取得できませんでした。再試行します:", err);
      retry(more);
    } finally {
      loading = false;
    }
  };

  const observer = new IntersectionObserver((entries) => {
    if (loading || !entries.some((e) => e.isIntersecting)) return;
    const more = document.getElementById("more");
    if (more) load(more);
  });

  const more = document.getElementById("more");
  if (more) observer.observe(more);
})();
