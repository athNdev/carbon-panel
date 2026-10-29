// SPDX-License-Identifier: MIT AND MPL-2.0
// This file includes DiscoPanel material under the MIT License,
// Copyright (c) 2025 Nicholas W. Heyer. The MIT license text is in LICENSE
// and still applies to that inherited material.
// Maintainer changes are licensed under the Mozilla Public License, v. 2.0.
// If a copy of the MPL was not distributed with this file, You can obtain
// one at https://mozilla.org/MPL/2.0/.


// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';

const websiteOrigin = process.env.WEBSITE_ORIGIN || 'https://athn.dev';
const websiteBase = process.env.WEBSITE_BASE_PATH || '/carbonpanel/';
const socialImage = new URL(websiteBase + 'assets/og.png?v=20261029h', websiteOrigin).href;

// https://astro.build/config
export default defineConfig({
	site: websiteOrigin,
	base: process.env.DOCS_BASE_PATH || '/carbonpanel/docs/',
	trailingSlash: 'always',
	integrations: [
		starlight({
			title: 'Carbon Panel',
			components: {
				SiteTitle: './src/components/SiteTitle.astro',
				Banner: './src/components/VersionBanner.astro',
			},
			logo: {
				dark: './public/brand-mark-light.svg',
				light: './public/brand-mark-dark.svg',
				alt: '',
			},
			customCss: ['./src/styles/custom.css'],
			head: [
				{ tag: 'meta', attrs: { property: 'og:image', content: socialImage } },
				{ tag: 'meta', attrs: { property: 'og:image:type', content: 'image/png' } },
				{ tag: 'meta', attrs: { property: 'og:image:width', content: '1200' } },
				{ tag: 'meta', attrs: { property: 'og:image:height', content: '630' } },
				{ tag: 'meta', attrs: { property: 'og:image:alt', content: 'Carbon Panel. Modpack Studio, datapacks, staged updates, and wake on join.' } },
				{ tag: 'meta', attrs: { name: 'twitter:image', content: socialImage } },
				{ tag: 'meta', attrs: { name: 'twitter:image:alt', content: 'Carbon Panel. Modpack Studio, datapacks, staged updates, and wake on join.' } },
			],
			favicon: '/favicon.png',
			social: [
				{ icon: 'github', label: 'GitHub', href: 'https://github.com/athNdev/carbonpanel' },
			],
			sidebar: [
				{ label: 'Documentation home', slug: '' },
				{ label: 'Introduction', slug: 'introduction' },
				{
					label: 'Getting Started',
					items: [
						{ label: 'Docker Compose', slug: 'getting-started/docker-compose' },
						{ label: 'Proxmox LXC', slug: 'getting-started/proxmox' },
						{ label: 'Prebuilt Binaries', slug: 'getting-started/prebuilt-binaries' },
						{ label: 'Building from Source', slug: 'getting-started/build-from-source' },
					],
				},
				{ label: 'Configuration', slug: 'configuration' },
				{
					label: 'Guides',
					items: [
						{ label: 'Server Backups', slug: 'guides/backups' },
						{
							label: 'OIDC',
							items: [
								{ label: 'Keycloak', slug: 'guides/oidc/keycloak' },
								{ label: 'Authelia', slug: 'guides/oidc/authelia' },
								{ label: 'Google', slug: 'guides/oidc/google' },
								{ label: 'Discord', slug: 'guides/oidc/discord' },
							],
						},
					],
				},
				{ label: 'FAQ', slug: 'faq' },
				{ label: 'Troubleshooting', slug: 'troubleshooting' },
				{ label: 'API Reference', slug: 'api' },
				{ label: 'Changelog', slug: 'changelog' },
				{ label: 'Documentation version', slug: 'versions' },
				{ label: 'Contributing', slug: 'contributing' },
			],
		}),
	],
});
