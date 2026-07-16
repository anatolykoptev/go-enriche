package maps

import (
	"strings"
	"testing"
)

// TestParseOrgPage_NewFormat verifies parsing of the new Yandex Maps org page
// format (2026-07+). The old regexes matched the JSON-LD WebSite schema
// "name":"Yandex Maps" instead of the business name, and used wrong field
// names for phone/url/address/hours. This test uses a fixture extracted from
// a real rendered Yandex Maps page (Supramen, СПб) to guard against regression.
func TestParseOrgPage_NewFormat(t *testing.T) {
	html := []byte(`<html><head><script type="application/ld+json">{"@context":"https://schema.org","@type":"WebSite","name":"Yandex Maps","url":"https://yandex.com/maps/"}</script></head><body><script>{ "shortTitle":"Supramen","fullAddress":"Saint Petersburg, Aleksandra Nevskogo Street, 12","country":"Russian Federation","postalCode":"191167","chain":{"id":"75219674380","name":"Supramen","seoname":"supramen","quantityInCity":1},"status":"open","businessLinks":[],"ratingData":{"ratingCount":1079,"ratingValue":5,"reviewCount":757},"sources":[{"id":"yandex","name":"Yandex","href":"https://www.yandex.com"}],"categories":[{"id":"184106394","name":"Restaurant","class":"restaurants","seoname":"restaurant","pluralName":"Restaurants"},{"id":"184106390","name":"cafe","class":"cafe","seoname":"cafe","pluralName":"Cafes"},{"id":"184106384","name":"bar, pub","class":"bars","seoname":"bar_pub","pluralName":"Bars, pubs"}],"phones":[{"number":"+7 (921) 323-28-94","type":"phone","value":"+79213232894"}],"features":[{"id":"average_bill2","value":"1000–1200 ₽","name":"average bill","type":"text","important":true},{"id":"food_delivery","value":true,"name":"food delivery","aref":"#yandex-eda","type":"bool","important":true},{"id":"coffee_to_go","value":true,"name":"coffee to go","type":"bool","important":true},{"id":"takeaway","value":true,"name":"takeout","type":"bool","important":true},{"id":"price_category","value":[{"id":"price_expensive","name":"above average"}],"name":"prices","type":"enum","important":true},{"id":"Possible_with_dog","value":true,"name":"Possible with a dog","type":"bool","important":true},{"id":"non_cash_tips","value":[{"id":"netmonet","name":"netmonet"}],"name":"types of non-cash tips","type":"enum","important":false},{"id":"gift_certificate","value":true,"name":"gift certificate","type":"bool","important":false},{"id":"good_place","value":true,"type":"bool","important":false},{"id":"caring_for_couriers_tea","value":true,"name":"Tea","type":"bool","important":false},{"id":"petfriendly","value":[{"id":"allowed_with_dogs_35_cm","name":"allowed with dogs up to 35 cm."}],"name":"petfriendly","type":"enum","important":false},{"id":"payment_method","value":[{"id":"cash","name":"cash"},{"id":"payment_card","name":"payment by card"},{"id":"qr_code","name":"QR code"}],"name":"payment method","type":"enum","important":false},{"id":"caring_for_couriers_coffee","value":true,"name":"Coffee","type":"bool","important":false},{"id":"music","value":[{"id":"pop_music","name":"pop"},{"id":"rnb","name":"r'n'b"},{"id":"rapping","name":"rap"},{"id":"bard","name":"bard"},{"id":"latin_music","name":"latina"},{"id":"rock_n_roll","name":"rock-n-roll"},{"id":"background_music","name":"background"}],"name":"music","type":"enum","important":false},{"id":"promotions","value":[{"id":"discounts","name":"discounts"},{"id":"promotions","name":"promotions"}],"name":"promotions","type":"enum","important":false},{"id":"caring_for_couriers_charging_station","value":true,"name":"Charging station","type":"bool","important":false},{"id":"business_lunch","value":true,"name":"business lunch","type":"bool","important":false},{"id":"toilet","value":true,"name":"toilet","type":"bool","important":false},{"id":"caring_for_couriers_wc","value":true,"name":"WC","type":"bool","important":false},{"id":"preliminary_registration","value":true,"name":"preliminary registration","type":"bool","important":false},{"id":"booking_by_alice","value":true,"aref":"#yandex-eda","type":"bool","important":false},{"id":"payment_by_credit_card","value":true,"name":"Credit card payment","aref":"#tomesto","type":"bool","important":false},{"id":"special_menu","value":[{"id":"seasonal_menu","name":"seasonal"}],"name":"special menu","type":"enum","important":false},{"id":"caring_for_couriers_water","value":true,"name":"Water","type":"bool","important":false},{"id":"type_cuisine","value":[{"id":"chinese_cuisine","name":"chinese"},{"id":"pan_asian_cuisine","name":"pan asian"},{"id":"japanese_cuisine","name":"japanese"},{"id":"asian_cuisine","name":"asian"},{"id":"vegetarian_cuisine","name":"vegetarian"}],"name":"cuisine","aref":"#tomesto","type":"enum","important":false},{"id":"wheelchair_access","value":[{"id":"unavailable","name":"unavailable"}],"name":"wheelchair accessibility","type":"enum","important":false},{"id":"wi_fi","value":true,"name":"Wi-Fi","aref":"#tomesto","type":"bool","important":false},{"id":"street_entrance","value":true,"type":"bool","important":false},{"id":"type_public_catering","value":[{"id":"panasian_restaurant","name":"Pan-asian restaurant"}],"name":"type of place","type":"enum","important":false},{"id":"wheelchair_accessible_vocabulary","value":[{"id":"wheelchair_accessible_na","name":"Not available"}],"name":"wheelchair accessible","type":"enum","important":false},{"id":"business lunch price","value":"560 ₽","name":"business lunch price","type":"text","important":false},{"id":"features_institution","value":[{"id":"animal_friendly","name":"animal friendly"},{"id":"allowed_with_a_laptop","name":"allowed with a laptop"},{"id":"open_kitchen","name":"open kitchen"}],"name":"features institution","type":"enum","important":false},{"id":"dimmed_lights","value":true,"name":"dimmed lights","type":"bool","important":false},{"id":"types_of_delivery","value":[{"id":"yandex_eda","name":"Yandex.Eda"}],"name":"types of delivery","type":"enum","important":false},{"id":"number_of_tables","value":"35–45","name":"number of tables","type":"text","important":false},{"id":"summer_terrace","value":true,"name":"summer terrace","type":"bool","important":false},{"id":"show_briefly_about_the_place_block","value":true,"type":"bool","important":false}],"mediaOrderTemplate":"p,v,p,v,v,p,v*,p*","featureGroups":[{"name":"Achievements","featureIds":["good_place"]},{"name":"Prices","featureIds":["price_category","average_bill2","business lunch price"]},{"name":"General information","featureIds":["types_of_delivery","type_cuisine","type_public_catering","non_cash_tips","gift_certificate","payment_by_credit_card","menu for children","number_of_tables","food_delivery","takeaway","promotions","payment_method","online_takeaway","business_lunch","wi_fi","coffee_to_go","breakfast","nursery"]},{"name":"Features","featureIds":["features_institution","summer_terrace","food_court1","sports_broadcasts","table_games","music","billiards","dancefloor","special_menu","craft_beer","karaoke","projector"]},{"name":"Accessibility","featureIds":["elevator_wheelchair_accessible","ramp","wheelchair_access","call_button","toilet_for_disabled","parking_disabled","automatic_door","wheelchair_accessible_vocabulary"]},{"name":"Courier-friendly","featureIds":["caring_for_couriers_water","caring_for_couriers_tea","caring_for_couriers_coffee","caring_for_couriers_charging_station","caring_for_couriers_tea_food_discount","caring_for_couriers_rest_area","caring_for_couriers_wc","caring_for_couriers_bool"]}],"businessProperties":{"has_verified_owner":true,"geoproduct_poi_color":"#FFA15E","geoproduct_poi_label_color":"#D4711C"},"seoname":"supramen","geoId":120680,"compositeAddress":{"country":"Russian Federation","locality":"Saint Petersburg","street":"Aleksandra Nevskogo Street","house":"12"},"modularCard":{"showClaimOrganization":true,"showTaxiButton":true,"showFeedbackButton":true,"showReviews":true,"showAddPhotoButton":true,"showCompetitors":false,"showDirectBanner":false,"showAdditionalAds":false,"showWorkHours":true,"showContacts":true,"showFriendsLiked":false,"showVisualPricesTab":true,"showHotelEnrichment":false,"promoBadgeFeature":[],"offerBadgeId":[],"showCourierButton":false,"hotelReviewsStubType":0},"modularPin":{"subtitleHints":[{"text":"5","type":"RATING","properties":{"rating":{"showGoodPlace":true}}},{"text":"Avg. bill: from 1000 ₽","type":"PRICE","properties":{"price":{"name":"Avg. bill","priceRange":{"lower":1000,"upper":1200,"text":"from 1000 ₽","currency":"₽"}}}}],"allowMultilineSubtitle":false},"modularSnippet":{"showNeurosummary":false,"showMentionedOnSite":false,"showTitle":"SHORT_TITLE","showAddress":"NO_ADDRESS","showCategory":"ALL_CATEGORIES","showRating":"FIVE_STAR_RATING","showPhoto":"GALLERY","showWorkHours":true,"showVerified":true,"showDistanceFromTransit":false,"showBookmark":true,"showEta":true,"showGeoproductOffer":true,"subtitleHint":[],"galleryButton":["MENU"],"shownGoodsNumber":3,"showDescription":false,"showCollectionBadge":true,"showFriendsLiked":false,"showCourierButton":false,"showYandexEatsOrderButton":false,"infoBlock":[],"serpActionButtons":[7,1,3,5,0],"awards":[0],"promoBadgeFeature":[],"offerBadgeId":[],"showHotelEnrichment":false},"tzOffset":10800,"workingTimeText":"daily, 11:00 AM–11:00 PM","workingTime":[[{"from":{"hours":11,"minutes":0},"to":{"hours":23,"minutes":0}}],[{"from":{"hours":11,"minutes":0},"to":{"hours":23,"minutes":0}}],[{"from":{"hours":11,"minutes":0},"to":{"hours":23,"minutes":0}}],[{"from":{"hours":11,"minutes":0},"to":{"hours":23,"minutes":0}}],[{"from":{"hours":11,"minutes":0},"to":{"hours":23,"minutes":0}}],[{"from":{"hours":11,"minutes":0},"to":{"hours":23,"minutes":0}}],[{"from":{"hours":11,"minutes":0},"to":{"hours":23,"minutes":0}}]],"currentWorkingStatus":{"isOpenNow":false,"text":"Closed until 11:00 AM","shortText":"Opens: 11:00 AM","tag":[]},"socialLinks":[{"type":"vkontakte","href":"https://vk.com/supramen_spb","readableHref":"vk.com/supramen_spb"},{"type":"telegram","href":"https://t.me/+gYzrmbj6T781MDgy","readableHref":"@+gYzrmbj6T781MDgy"}],"urls":["https://supramen.rest/?utm_campaign=firstpage&utm_medium=main_link&utm_source=yandex_maps"],"advert":{"title":"","text":"","url":"","ordInfo":{"client":{"id":"316327883","tin":"7814827642","name":"ООО \"ВКУС\""}},"isBko":false,"actionButtons":[{"type":"url","title":"Order online","value":"https://supramen.rest/?utm_campaign=main_button&utm_medium=main_link&utm_source=yandex_maps"}],"highlighted":true,"products":[{"title":"Сырный рамен","img":"https://avatars.mds.yand }</script></body></html>`)

	od := parseOrgPage(html)

	if od.Status != PlaceOpen {
		t.Errorf("status = %q, want %q", od.Status, PlaceOpen)
	}
	if od.Name != "Supramen" {
		t.Errorf("name = %q, want %q (must not be \"Yandex Maps\" from JSON-LD WebSite schema)", od.Name, "Supramen")
	}
	if od.Address != "Saint Petersburg, Aleksandra Nevskogo Street, 12" {
		t.Errorf("address = %q", od.Address)
	}
	if od.Phone != "+7 (921) 323-28-94" {
		t.Errorf("phone = %q, want %q", od.Phone, "+7 (921) 323-28-94")
	}
	// Hours may contain a unicode narrow no-break space (U+202F) — check prefix.
	if !strings.HasPrefix(od.Hours, "Closed until 11:00") {
		t.Errorf("hours = %q, want prefix %q", od.Hours, "Closed until 11:00")
	}
	if od.Rating != 5 {
		t.Errorf("rating = %f, want 5", od.Rating)
	}
	if !strings.HasPrefix(od.Website, "https://supramen.rest/") {
		t.Errorf("website = %q, want prefix %q", od.Website, "https://supramen.rest/")
	}
	if len(od.Categories) == 0 {
		t.Errorf("categories = empty, want at least 1")
	}
	found := false
	for _, c := range od.Categories {
		if c == "Restaurant" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("categories = %v, want to contain \"Restaurant\"", od.Categories)
	}
}

// TestParseOrgPage_NewFormat_NameFallback verifies that when shortTitle is
// absent, chain.name is used instead of falling through to the JSON-LD
// WebSite "name" trap.
func TestParseOrgPage_NewFormat_NameFallback(t *testing.T) {
	html := []byte(`{"@type":"WebSite","name":"Yandex Maps","url":"https://yandex.com/maps/"}{"chain":{"id":"123","name":"Ramen Rebel","seoname":"ramen-rebel"},"status":"open"}`)

	od := parseOrgPage(html)

	if od.Name != "Ramen Rebel" {
		t.Errorf("name = %q, want %q (chain.name fallback)", od.Name, "Ramen Rebel")
	}
}
