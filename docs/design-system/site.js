const themeButton = document.querySelector('[data-theme-toggle]');
const motionButton = document.querySelector('[data-replay]');
const motionObject = document.querySelector('[data-motion-object]');
const motionStatus = document.querySelector('[data-motion-status]');

themeButton?.addEventListener('click', () => {
	const light = document.documentElement.dataset.carbonTheme !== 'light';
	document.documentElement.dataset.carbonTheme = light ? 'light' : 'dark';
	themeButton.setAttribute('aria-label', light ? 'Switch to dark preview' : 'Switch to light preview');
	themeButton.innerHTML = light ? 'Dark preview <span aria-hidden="true">◑</span>' : 'Light preview <span aria-hidden="true">◐</span>';
});

motionButton?.addEventListener('click', () => {
	if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) {
		motionStatus.textContent = 'Reduced motion is enabled; the result is shown without animation.';
		return;
	}
	motionObject.classList.remove('is-running');
	void motionObject.offsetWidth;
	motionObject.classList.add('is-running');
	motionStatus.textContent = 'Animation replayed: 200 ms · 6 px · ease out.';
});
