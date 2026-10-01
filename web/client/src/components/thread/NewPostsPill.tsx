import { useEffect, useState } from 'preact/hooks';

// NewPostsPill is the "3 new posts — show" button at the foot of the post
// list (docs/06 "Live updates"), drawn while count posts are held behind it.
// The count is announced politely, a second after it last changed, so a
// thread that is busy is not read out post by post.
export function NewPostsPill({ count, onShow }: { count: number; onShow: () => void }) {
  const what = `${count} new ${count === 1 ? 'post' : 'posts'}`;
  const [said, setSaid] = useState('');
  useEffect(() => {
    const t = setTimeout(() => setSaid(count ? what : ''), 1000);
    return () => clearTimeout(t);
  }, [what]);
  return (
    <>
      {count > 0 && (
        <button type="button" class="new-posts" onClick={onShow}>
          {what} — show
        </button>
      )}
      <p class="visually-hidden" aria-live="polite">
        {said}
      </p>
    </>
  );
}
